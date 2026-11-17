package handlers

import (
	"sort"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	dvcrypto "github.com/muecahit94/dockvault/crypto"
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

	const activityDays = 90
	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(activityDays - 1))
	var periodLogs []models.BackupLog
	s.db.Where("started_at >= ?", dayStart).Find(&periodLogs)

	dailyMap := make(map[string]*dailyPoint, activityDays)
	dailyActivity := make([]dailyPoint, activityDays)
	for i := 0; i < activityDays; i++ {
		d := dayStart.AddDate(0, 0, i).Format("2006-01-02")
		dailyActivity[i] = dailyPoint{Date: d}
		dailyMap[d] = &dailyActivity[i]
	}
	for _, item := range periodLogs {
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

	// Storage analytics and security overview.
	var providers []models.StorageProvider
	s.db.Order("is_default desc, id asc").Find(&providers)

	type providerStat struct {
		ProviderID  uint   `json:"providerId"`
		Name        string `json:"name"`
		Type        string `json:"type"`
		AccessTier  string `json:"accessTier,omitempty"`
		Encrypted   bool   `json:"encrypted"`
		BackupCount int    `json:"backupCount"`
		TotalBytes  int64  `json:"totalBytes"`
	}
	provMap := make(map[uint]*providerStat, len(providers))
	providerBreakdown := make([]*providerStat, 0, len(providers))
	encryptedProviders := 0
	for _, p := range providers {
		if p.EncryptBackups {
			encryptedProviders++
		}
		ps := &providerStat{
			ProviderID: p.ID,
			Name:       p.Name,
			Type:       p.Type,
			AccessTier: p.AzureAccessTier,
			Encrypted:  p.EncryptBackups,
		}
		provMap[p.ID] = ps
		providerBreakdown = append(providerBreakdown, ps)
	}

	var successLogs []models.BackupLog
	s.db.Where("status = ?", "success").Order("started_at desc").Find(&successLogs)

	type targetStat struct {
		TargetName  string `json:"targetName"`
		TargetType  string `json:"targetType"`
		BackupCount int    `json:"backupCount"`
		LatestBytes int64  `json:"latestBytes"`
		TotalBytes  int64  `json:"totalBytes"`
	}
	targetMap := make(map[string]*targetStat)
	encryptedBackups := 0
	gzipCount := 0
	zstdCount := 0
	for _, l := range successLogs {
		if l.Encrypted {
			encryptedBackups++
		}
		if dvcrypto.NormalizeCompression(l.Compression) == "zstd" || dvcrypto.IsZstdFile(l.FileName) {
			zstdCount++
		} else {
			gzipCount++
		}
		if ps, ok := provMap[l.StorageProviderID]; ok {
			ps.BackupCount++
			ps.TotalBytes += l.SizeBytes
		}
		tKey := l.TargetType + ":" + l.TargetName
		ts, ok := targetMap[tKey]
		if !ok {
			ts = &targetStat{
				TargetName:  l.TargetName,
				TargetType:  l.TargetType,
				LatestBytes: l.SizeBytes,
			}
			targetMap[tKey] = ts
		}
		ts.BackupCount++
		ts.TotalBytes += l.SizeBytes
	}

	topTargets := make([]*targetStat, 0, len(targetMap))
	for _, ts := range targetMap {
		topTargets = append(topTargets, ts)
	}
	sort.Slice(topTargets, func(i, j int) bool {
		return topTargets[i].TotalBytes > topTargets[j].TotalBytes
	})
	if len(topTargets) > 8 {
		topTargets = topTargets[:8]
	}

	authMode := "none"
	if s.isAuthEnabled() {
		_, _, authMode = s.authenticateRequest(c)
		if authMode == "" {
			authMode = "local"
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
		"storageAnalytics": fiber.Map{
			"disk":       readDiskUsage(s.cfg.BackupDir),
			"byProvider": providerBreakdown,
			"byTarget":   topTargets,
			"compression": fiber.Map{
				"gzip": gzipCount,
				"zstd": zstdCount,
			},
		},
		"securityOverview": fiber.Map{
			"authEnabled":            s.isAuthEnabled(),
			"authMode":               authMode,
			"trustedProxyHeader":     s.trustedProxyHeader(),
			"httpsActive":            isHTTPSRequest(c),
			"frameAncestorsSet":      s.cfg.FrameAncestors != "",
			"encryptedProviders":     encryptedProviders,
			"totalProviders":         len(providers),
			"encryptedBackups":       encryptedBackups,
			"totalSuccessfulBackups": len(successLogs),
			"globalKeyConfigured":    strings.TrimSpace(s.cfg.EncryptionKey) != "",
		},
	})
}
