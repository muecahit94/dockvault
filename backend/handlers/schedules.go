package handlers

import (
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	dvcrypto "github.com/muecahit94/dockvault/crypto"
	"github.com/muecahit94/dockvault/docker"
	"github.com/muecahit94/dockvault/models"
	"github.com/muecahit94/dockvault/scheduler"
)

type schedulePayload struct {
	Name               string `json:"name"`
	TargetType         string `json:"targetType"`
	TargetID           string `json:"targetId"`
	TargetName         string `json:"targetName"`
	CronExpr           string `json:"cronExpr"`
	StorageProviderID  uint   `json:"storageProviderId"`
	SecondaryStorageID *uint  `json:"secondaryStorageId"`
	PauseContainer     *bool  `json:"pauseContainer"`
	RetentionPolicy    string `json:"retentionPolicy"`
	RetentionCount     *int   `json:"retentionCount"`
	KeepDaily          *int   `json:"keepDaily"`
	KeepWeekly         *int   `json:"keepWeekly"`
	KeepMonthly        *int   `json:"keepMonthly"`
	PreCommand         string `json:"preCommand"`
	PostCommand        string `json:"postCommand"`
	ExcludePatterns    string `json:"excludePatterns"`
	Compression        string `json:"compression"`
	DBHotDump          *bool  `json:"dbHotDump"`
	Enabled            *bool  `json:"enabled"`
	AlertAfterHours    *int   `json:"alertAfterHours"`
}

type scheduleResponse struct {
	models.BackupSchedule
	NextRunAt     *time.Time `json:"nextRunAt"`
	LastSuccessAt *time.Time `json:"lastSuccessAt"`
	Stale         bool       `json:"stale"`
}

func (s *Server) toScheduleResponse(sched models.BackupSchedule) scheduleResponse {
	last := scheduler.LastSuccessAt(s.db, sched.ID)
	return scheduleResponse{
		BackupSchedule: sched,
		NextRunAt:      s.schedMgr.NextRun(sched.ID),
		LastSuccessAt:  last,
		Stale:          scheduler.IsStale(sched, last, time.Now()),
	}
}

// ListSchedules returns all backup schedules along with their next calculated run time.
func (s *Server) ListSchedules(c *fiber.Ctx) error {
	var schedules []models.BackupSchedule
	if err := s.db.Preload("StorageProvider").Preload("SecondaryStorageProvider").Order("id desc").Find(&schedules).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	out := make([]scheduleResponse, 0, len(schedules))
	for _, item := range schedules {
		out = append(out, s.toScheduleResponse(item))
	}
	return c.JSON(fiber.Map{"schedules": out})
}

// PreviewCron validates a cron expression and returns its next scheduled execution time.
func (s *Server) PreviewCron(c *fiber.Ctx) error {
	var body struct {
		CronExpr string `json:"cronExpr"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON body"})
	}
	next, err := scheduler.ValidateCron(strings.TrimSpace(body.CronExpr))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"valid": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"valid":     true,
		"nextRunAt": next,
	})
}

// CreateSchedule creates a new recurring backup schedule and activates it in the cron runner.
func (s *Server) CreateSchedule(c *fiber.Ctx) error {
	var body schedulePayload
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON body"})
	}

	targetType := strings.TrimSpace(body.TargetType)
	if targetType != "container" && targetType != "stack" && targetType != "volume" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "targetType must be 'container', 'stack', or 'volume'"})
	}
	targetID := strings.TrimSpace(body.TargetID)
	if targetID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "targetId is required"})
	}
	cronExpr := strings.TrimSpace(body.CronExpr)
	if _, err := scheduler.ValidateCron(cronExpr); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	var provider models.StorageProvider
	if body.StorageProviderID > 0 {
		if err := s.db.First(&provider, body.StorageProviderID).Error; err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "selected storage provider does not exist"})
		}
	} else {
		if err := s.db.Where("is_default = ?", true).First(&provider).Error; err != nil {
			if err := s.db.Order("id asc").First(&provider).Error; err != nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "no storage provider available"})
			}
		}
	}

	var secID *uint
	if body.SecondaryStorageID != nil && *body.SecondaryStorageID > 0 {
		if *body.SecondaryStorageID == provider.ID {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "secondary storage destination must differ from primary"})
		}
		var sec models.StorageProvider
		if err := s.db.First(&sec, *body.SecondaryStorageID).Error; err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "selected secondary storage provider does not exist"})
		}
		secID = body.SecondaryStorageID
	}

	targetName := strings.TrimSpace(body.TargetName)
	if targetName == "" {
		targetName = targetID
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = targetName + " backup"
	}

	pause := true
	if body.PauseContainer != nil {
		pause = *body.PauseContainer
	}
	retPolicy := strings.ToLower(strings.TrimSpace(body.RetentionPolicy))
	if retPolicy != "gfs" {
		retPolicy = "count"
	}
	retention := 7
	if body.RetentionCount != nil && *body.RetentionCount >= 0 {
		retention = *body.RetentionCount
	}
	keepDaily, keepWeekly, keepMonthly := 7, 4, 3
	if body.KeepDaily != nil && *body.KeepDaily >= 0 {
		keepDaily = *body.KeepDaily
	}
	if body.KeepWeekly != nil && *body.KeepWeekly >= 0 {
		keepWeekly = *body.KeepWeekly
	}
	if body.KeepMonthly != nil && *body.KeepMonthly >= 0 {
		keepMonthly = *body.KeepMonthly
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}

	dbHotDump := false
	if body.DBHotDump != nil {
		dbHotDump = *body.DBHotDump
	}
	alertAfter := 0
	if body.AlertAfterHours != nil && *body.AlertAfterHours > 0 {
		alertAfter = *body.AlertAfterHours
	}
	excludes := strings.Join(docker.ParseExcludePatterns(body.ExcludePatterns), "\n")

	sched := models.BackupSchedule{
		Name:               name,
		TargetType:         targetType,
		TargetID:           targetID,
		TargetName:         targetName,
		CronExpr:           cronExpr,
		StorageProviderID:  provider.ID,
		SecondaryStorageID: secID,
		PauseContainer:     pause,
		RetentionPolicy:    retPolicy,
		RetentionCount:     retention,
		KeepDaily:          keepDaily,
		KeepWeekly:         keepWeekly,
		KeepMonthly:        keepMonthly,
		PreCommand:         strings.TrimSpace(body.PreCommand),
		PostCommand:        strings.TrimSpace(body.PostCommand),
		ExcludePatterns:    excludes,
		Compression:        dvcrypto.NormalizeCompression(body.Compression),
		DBHotDump:          dbHotDump,
		Enabled:            enabled,
		AlertAfterHours:    alertAfter,
	}

	if err := s.db.Create(&sched).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if err := s.schedMgr.Sync(); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	s.db.Preload("StorageProvider").Preload("SecondaryStorageProvider").First(&sched, sched.ID)
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"schedule": s.toScheduleResponse(sched),
	})
}

// UpdateSchedule updates an existing backup schedule and resynchronizes cron jobs.
func (s *Server) UpdateSchedule(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid schedule id"})
	}

	var sched models.BackupSchedule
	if err := s.db.First(&sched, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "schedule not found"})
	}

	var body schedulePayload
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON body"})
	}

	if v := strings.TrimSpace(body.Name); v != "" {
		sched.Name = v
	}
	if v := strings.TrimSpace(body.TargetType); v != "" {
		if v != "container" && v != "stack" && v != "volume" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "targetType must be 'container', 'stack', or 'volume'"})
		}
		sched.TargetType = v
	}
	if v := strings.TrimSpace(body.TargetID); v != "" {
		sched.TargetID = v
	}
	if v := strings.TrimSpace(body.TargetName); v != "" {
		sched.TargetName = v
	}
	if v := strings.TrimSpace(body.CronExpr); v != "" {
		if _, err := scheduler.ValidateCron(v); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		sched.CronExpr = v
	}
	if body.StorageProviderID > 0 {
		var provider models.StorageProvider
		if err := s.db.First(&provider, body.StorageProviderID).Error; err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "selected storage provider does not exist"})
		}
		sched.StorageProviderID = body.StorageProviderID
	}
	if body.SecondaryStorageID != nil {
		if *body.SecondaryStorageID == 0 {
			sched.SecondaryStorageID = nil
		} else {
			if *body.SecondaryStorageID == sched.StorageProviderID {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "secondary storage destination must differ from primary"})
			}
			var sec models.StorageProvider
			if err := s.db.First(&sec, *body.SecondaryStorageID).Error; err != nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "selected secondary storage provider does not exist"})
			}
			sched.SecondaryStorageID = body.SecondaryStorageID
		}
	}
	if body.PauseContainer != nil {
		sched.PauseContainer = *body.PauseContainer
	}
	if rp := strings.ToLower(strings.TrimSpace(body.RetentionPolicy)); rp == "count" || rp == "gfs" {
		sched.RetentionPolicy = rp
	}
	if body.RetentionCount != nil && *body.RetentionCount >= 0 {
		sched.RetentionCount = *body.RetentionCount
	}
	if body.KeepDaily != nil && *body.KeepDaily >= 0 {
		sched.KeepDaily = *body.KeepDaily
	}
	if body.KeepWeekly != nil && *body.KeepWeekly >= 0 {
		sched.KeepWeekly = *body.KeepWeekly
	}
	if body.KeepMonthly != nil && *body.KeepMonthly >= 0 {
		sched.KeepMonthly = *body.KeepMonthly
	}
	sched.PreCommand = strings.TrimSpace(body.PreCommand)
	sched.PostCommand = strings.TrimSpace(body.PostCommand)
	sched.ExcludePatterns = strings.Join(docker.ParseExcludePatterns(body.ExcludePatterns), "\n")
	if strings.TrimSpace(body.Compression) != "" {
		sched.Compression = dvcrypto.NormalizeCompression(body.Compression)
	}
	if body.DBHotDump != nil {
		sched.DBHotDump = *body.DBHotDump
	}
	if body.Enabled != nil {
		sched.Enabled = *body.Enabled
	}
	if body.AlertAfterHours != nil && *body.AlertAfterHours >= 0 {
		sched.AlertAfterHours = *body.AlertAfterHours
	}

	if err := s.db.Save(&sched).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if err := s.schedMgr.Sync(); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	s.db.Preload("StorageProvider").Preload("SecondaryStorageProvider").First(&sched, sched.ID)
	return c.JSON(fiber.Map{
		"schedule": s.toScheduleResponse(sched),
	})
}

// ToggleSchedule flips a schedule's enabled flag.
func (s *Server) ToggleSchedule(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid schedule id"})
	}

	var sched models.BackupSchedule
	if err := s.db.First(&sched, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "schedule not found"})
	}

	sched.Enabled = !sched.Enabled
	if err := s.db.Save(&sched).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if err := s.schedMgr.Sync(); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	s.db.Preload("StorageProvider").Preload("SecondaryStorageProvider").First(&sched, sched.ID)
	return c.JSON(fiber.Map{
		"schedule": s.toScheduleResponse(sched),
	})
}

// TriggerSchedule runs a configured schedule immediately.
func (s *Server) TriggerSchedule(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid schedule id"})
	}
	if err := s.schedMgr.TriggerNow(uint(id)); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"triggered": true})
}

// SyncGitOpsSchedules imports/updates schedules declared via dockvault.schedule Docker labels.
func (s *Server) SyncGitOpsSchedules(c *fiber.Ctx) error {
	res, err := s.schedMgr.SyncGitOpsSchedules(c.UserContext())
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(res)
}

// DeleteSchedule deletes a backup schedule and removes it from the cron runner.
func (s *Server) DeleteSchedule(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid schedule id"})
	}
	if err := s.db.Delete(&models.BackupSchedule{}, id).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	_ = s.schedMgr.Sync()
	return c.JSON(fiber.Map{"deleted": true})
}

type cloneTarget struct {
	TargetType string `json:"targetType"`
	TargetID   string `json:"targetId"`
	TargetName string `json:"targetName"`
}

// CloneSchedule duplicates a schedule's policy. Without targets it creates a paused copy of the
// same target; with targets it applies the policy to each (skipping targets already scheduled).
func (s *Server) CloneSchedule(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid schedule id"})
	}
	var src models.BackupSchedule
	if err := s.db.First(&src, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "schedule not found"})
	}

	var body struct {
		Targets []cloneTarget `json:"targets"`
	}
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&body); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON body"})
		}
	}

	newFrom := func(t cloneTarget, name string, enabled bool) models.BackupSchedule {
		cp := src
		cp.ID = 0
		cp.StorageProvider = nil
		cp.SecondaryStorageProvider = nil
		cp.TargetType = t.TargetType
		cp.TargetID = t.TargetID
		cp.TargetName = t.TargetName
		cp.Name = name
		cp.Enabled = enabled
		cp.ManagedByLabel = false
		cp.LastRunAt = nil
		cp.LastStatus = ""
		cp.StaleAlertedAt = nil
		cp.CreatedAt = time.Time{}
		cp.UpdatedAt = time.Time{}
		return cp
	}

	var toCreate []models.BackupSchedule
	skipped := []string{}
	if len(body.Targets) == 0 {
		// A paused copy avoids two identical jobs contending for the same target lock.
		toCreate = append(toCreate, newFrom(cloneTarget{
			TargetType: src.TargetType, TargetID: src.TargetID, TargetName: src.TargetName,
		}, src.Name+" (copy)", false))
	} else {
		seen := make(map[string]bool)
		for _, t := range body.Targets {
			t.TargetType = strings.TrimSpace(t.TargetType)
			t.TargetID = strings.TrimSpace(t.TargetID)
			t.TargetName = strings.TrimSpace(t.TargetName)
			if t.TargetType != "container" && t.TargetType != "stack" && t.TargetType != "volume" {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "targetType must be 'container', 'stack', or 'volume'"})
			}
			if t.TargetID == "" {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "targetId is required for every target"})
			}
			if t.TargetName == "" {
				t.TargetName = t.TargetID
			}
			key := t.TargetType + ":" + t.TargetID
			if seen[key] {
				continue
			}
			seen[key] = true
			var existing int64
			s.db.Model(&models.BackupSchedule{}).
				Where("target_type = ? AND target_id = ?", t.TargetType, t.TargetID).
				Count(&existing)
			if existing > 0 {
				skipped = append(skipped, t.TargetName)
				continue
			}
			toCreate = append(toCreate, newFrom(t, t.TargetName+" backup", src.Enabled))
		}
	}

	created := make([]scheduleResponse, 0, len(toCreate))
	if len(toCreate) > 0 {
		if err := s.db.Create(&toCreate).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		if err := s.schedMgr.Sync(); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		for _, sc := range toCreate {
			s.db.Preload("StorageProvider").Preload("SecondaryStorageProvider").First(&sc, sc.ID)
			created = append(created, s.toScheduleResponse(sc))
		}
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"schedules": created,
		"skipped":   skipped,
	})
}
