package handlers

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/muecahit94/dockvault/models"
)

// GetHealth returns application liveness and Docker daemon connectivity.
func (s *Server) GetHealth(c *fiber.Ctx) error {
	dockerStatus := s.dockerSvc.Status(c.UserContext())
	return c.JSON(fiber.Map{
		"status":  "ok",
		"version": s.cfg.Version,
		"docker":  dockerStatus,
	})
}

// GetDashboard aggregates summary metrics, recent backups, and upcoming scheduled runs.
func (s *Server) GetDashboard(c *fiber.Ctx) error {
	dockerStatus := s.dockerSvc.Status(c.UserContext())

	containersTotal := 0
	containersRunning := 0
	stacksTotal := 0
	if dockerStatus.Connected {
		if containers, err := s.dockerSvc.ListContainers(c.UserContext(), true); err == nil {
			containersTotal = len(containers)
			for _, ctr := range containers {
				if ctr.State == "running" {
					containersRunning++
				}
			}
		}
		if stacks, err := s.dockerSvc.ListStacks(c.UserContext(), true); err == nil {
			stacksTotal = len(stacks)
		}
	}

	var activeSchedules int64
	s.db.Model(&models.BackupSchedule{}).Where("enabled = ?", true).Count(&activeSchedules)

	var totalBackups int64
	s.db.Model(&models.BackupLog{}).Count(&totalBackups)

	var successfulBackups int64
	s.db.Model(&models.BackupLog{}).Where("status = ?", "success").Count(&successfulBackups)

	var failedBackups int64
	s.db.Model(&models.BackupLog{}).Where("status = ?", "failed").Count(&failedBackups)

	var totalBackupBytes int64
	s.db.Model(&models.BackupLog{}).
		Where("status = ?", "success").
		Select("COALESCE(SUM(size_bytes), 0)").
		Scan(&totalBackupBytes)

	var storageProviders int64
	s.db.Model(&models.StorageProvider{}).Count(&storageProviders)

	var recentBackups []models.BackupLog
	s.db.Order("started_at desc").Limit(8).Find(&recentBackups)

	var schedules []models.BackupSchedule
	s.db.Preload("StorageProvider").Where("enabled = ?", true).Order("id asc").Find(&schedules)

	type upcomingItem struct {
		ID           uint       `json:"id"`
		Name         string     `json:"name"`
		TargetType   string     `json:"targetType"`
		TargetName   string     `json:"targetName"`
		CronExpr     string     `json:"cronExpr"`
		NextRunAt    *time.Time `json:"nextRunAt"`
		LastStatus   string     `json:"lastStatus"`
		StorageTitle string     `json:"storageTitle"`
	}

	upcoming := make([]upcomingItem, 0, len(schedules))
	for _, sched := range schedules {
		storageTitle := ""
		if sched.StorageProvider != nil {
			storageTitle = sched.StorageProvider.Name
		}
		upcoming = append(upcoming, upcomingItem{
			ID:           sched.ID,
			Name:         sched.Name,
			TargetType:   sched.TargetType,
			TargetName:   sched.TargetName,
			CronExpr:     sched.CronExpr,
			NextRunAt:    s.schedMgr.NextRun(sched.ID),
			LastStatus:   sched.LastStatus,
			StorageTitle: storageTitle,
		})
	}

	type dailyPoint struct {
		Date    string `json:"date"`
		Success int    `json:"success"`
		Failed  int    `json:"failed"`
		Bytes   int64  `json:"bytes"`
	}

	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -6)
	var weekLogs []models.BackupLog
	s.db.Where("started_at >= ?", dayStart).Find(&weekLogs)

	dailyMap := make(map[string]*dailyPoint, 7)
	dailyActivity := make([]dailyPoint, 7)
	for i := 0; i < 7; i++ {
		d := dayStart.AddDate(0, 0, i).Format("2006-01-02")
		dailyActivity[i] = dailyPoint{Date: d}
		dailyMap[d] = &dailyActivity[i]
	}
	for _, item := range weekLogs {
		key := item.StartedAt.UTC().Format("2006-01-02")
		if pt, ok := dailyMap[key]; ok {
			switch item.Status {
			case "success":
				pt.Success++
				pt.Bytes += item.SizeBytes
			case "failed":
				pt.Failed++
			}
		}
	}

	return c.JSON(fiber.Map{
		"version":     s.cfg.Version,
		"helperImage": s.cfg.HelperImage,
		"backupDir":   s.cfg.BackupDir,
		"docker":      dockerStatus,
		"stats": fiber.Map{
			"containersTotal":   containersTotal,
			"containersRunning": containersRunning,
			"stacksTotal":       stacksTotal,
			"activeSchedules":   activeSchedules,
			"totalBackups":      totalBackups,
			"successfulBackups": successfulBackups,
			"failedBackups":     failedBackups,
			"totalBackupBytes":  totalBackupBytes,
			"storageProviders":  storageProviders,
		},
		"recentBackups":     recentBackups,
		"upcomingSchedules": upcoming,
		"dailyActivity":     dailyActivity,
	})
}
