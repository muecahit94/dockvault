package scheduler

import (
	"context"
	"fmt"
	"log"
	"strings"

	dvcrypto "github.com/muecahit94/dockvault/crypto"
	"github.com/muecahit94/dockvault/docker"
	"github.com/muecahit94/dockvault/models"
)

// GitOpsCandidate describes a discovered container or Compose stack annotated with dockvault.schedule.
type GitOpsCandidate struct {
	TargetType      string
	TargetID        string
	TargetName      string
	CronExpr        string
	ExcludePatterns string
	RetentionCount  int
	Compression     string
	DBHotDump       bool
}

// GitOpsSyncResult summarizes schedules synced from Docker container labels.
type GitOpsSyncResult struct {
	Created   int                     `json:"created"`
	Updated   int                     `json:"updated"`
	Skipped   int                     `json:"skipped"`
	Schedules []models.BackupSchedule `json:"schedules"`
}

// SyncGitOpsSchedules inspects running/stopped Docker targets for dockvault.schedule labels and upserts managed schedules.
func (m *Manager) SyncGitOpsSchedules(ctx context.Context) (*GitOpsSyncResult, error) {
	stacks, err := m.dockerSvc.ListStacks(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("list stacks for gitops sync: %w", err)
	}
	containers, err := m.dockerSvc.ListContainers(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("list containers for gitops sync: %w", err)
	}

	candidates := collectGitOpsCandidates(stacks, containers)
	return m.applyGitOpsCandidates(candidates)
}

func collectGitOpsCandidates(stacks []docker.ComposeStackInfo, containers []docker.ContainerInfo) []GitOpsCandidate {
	var out []GitOpsCandidate
	for _, st := range stacks {
		if st.GitOpsSchedule == "" {
			continue
		}
		ret := st.GitOpsRetention
		if ret <= 0 {
			ret = 7
		}
		dbDump := len(st.DetectedDBs) > 0
		if st.GitOpsDBHotDump != nil {
			dbDump = *st.GitOpsDBHotDump
		}
		out = append(out, GitOpsCandidate{
			TargetType:      "stack",
			TargetID:        st.Name,
			TargetName:      st.Name,
			CronExpr:        st.GitOpsSchedule,
			ExcludePatterns: st.GitOpsExcludePaths,
			RetentionCount:  ret,
			Compression:     dvcrypto.NormalizeCompression(st.GitOpsCompression),
			DBHotDump:       dbDump,
		})
	}

	for _, c := range containers {
		if c.ComposeProject != "" || c.Excluded || c.IsSelf || c.GitOpsSchedule == "" {
			continue
		}
		ret := c.GitOpsRetention
		if ret <= 0 {
			ret = 7
		}
		dbDump := c.DetectedDB != ""
		if c.GitOpsDBHotDump != nil {
			dbDump = *c.GitOpsDBHotDump
		}
		out = append(out, GitOpsCandidate{
			TargetType:      "container",
			TargetID:        c.Name,
			TargetName:      c.Name,
			CronExpr:        c.GitOpsSchedule,
			ExcludePatterns: c.GitOpsExcludePaths,
			RetentionCount:  ret,
			Compression:     dvcrypto.NormalizeCompression(c.GitOpsCompression),
			DBHotDump:       dbDump,
		})
	}
	return out
}

func (m *Manager) applyGitOpsCandidates(candidates []GitOpsCandidate) (*GitOpsSyncResult, error) {
	res := &GitOpsSyncResult{}
	if len(candidates) == 0 {
		return res, nil
	}

	var defaultProvider models.StorageProvider
	if err := m.db.Where("is_default = ?", true).First(&defaultProvider).Error; err != nil {
		if err := m.db.Order("id asc").First(&defaultProvider).Error; err != nil {
			return nil, fmt.Errorf("no storage provider available for GitOps schedules: %w", err)
		}
	}

	for _, cand := range candidates {
		cronExpr := strings.TrimSpace(cand.CronExpr)
		if _, err := ValidateCron(cronExpr); err != nil {
			log.Printf("dockvault: skip invalid GitOps cron %q on %s %s: %v", cronExpr, cand.TargetType, cand.TargetName, err)
			res.Skipped++
			continue
		}

		comp := dvcrypto.NormalizeCompression(cand.Compression)
		var existing models.BackupSchedule
		err := m.db.Where("target_type = ? AND target_id = ? AND managed_by_label = ?", cand.TargetType, cand.TargetID, true).
			First(&existing).Error
		if err == nil {
			existing.TargetName = cand.TargetName
			existing.CronExpr = cronExpr
			existing.ExcludePatterns = cand.ExcludePatterns
			existing.Compression = comp
			existing.DBHotDump = cand.DBHotDump
			if cand.RetentionCount > 0 {
				existing.RetentionCount = cand.RetentionCount
			}
			if saveErr := m.db.Save(&existing).Error; saveErr != nil {
				return nil, saveErr
			}
			res.Updated++
			res.Schedules = append(res.Schedules, existing)
			continue
		}

		created := models.BackupSchedule{
			Name:              cand.TargetName + " (GitOps)",
			TargetType:        cand.TargetType,
			TargetID:          cand.TargetID,
			TargetName:        cand.TargetName,
			CronExpr:          cronExpr,
			StorageProviderID: defaultProvider.ID,
			PauseContainer:    true,
			RetentionPolicy:   "count",
			RetentionCount:    cand.RetentionCount,
			KeepDaily:         7,
			KeepWeekly:        4,
			KeepMonthly:       3,
			ExcludePatterns:   cand.ExcludePatterns,
			Compression:       comp,
			DBHotDump:         cand.DBHotDump,
			ManagedByLabel:    true,
			Enabled:           true,
		}
		if createErr := m.db.Create(&created).Error; createErr != nil {
			return nil, createErr
		}
		res.Created++
		res.Schedules = append(res.Schedules, created)
	}

	if err := m.Sync(); err != nil {
		return res, err
	}
	return res, nil
}
