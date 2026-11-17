package docker

import (
	"fmt"
	"sort"

	"github.com/muecahit94/dockvault/models"
)

// RetentionPolicyConfig defines either count-based or GFS (daily/weekly/monthly) pruning rules.
type RetentionPolicyConfig struct {
	Policy      string // "count" or "gfs"
	Count       int
	KeepDaily   int
	KeepWeekly  int
	KeepMonthly int
}

// SelectLogsToDelete returns the subset of successful BackupLog entries that should be pruned.
// Pinned backups are always protected and never returned for deletion.
func SelectLogsToDelete(logs []models.BackupLog, cfg RetentionPolicyConfig) []models.BackupLog {
	unpinned := make([]models.BackupLog, 0, len(logs))
	for _, entry := range logs {
		if entry.Pinned || entry.Status != "success" {
			continue
		}
		unpinned = append(unpinned, entry)
	}

	sort.Slice(unpinned, func(i, j int) bool {
		return unpinned[i].StartedAt.After(unpinned[j].StartedAt)
	})

	if cfg.Policy == "gfs" {
		return selectGFSToDelete(unpinned, cfg.KeepDaily, cfg.KeepWeekly, cfg.KeepMonthly)
	}

	if cfg.Count <= 0 || len(unpinned) <= cfg.Count {
		return nil
	}
	return unpinned[cfg.Count:]
}

func selectGFSToDelete(sorted []models.BackupLog, keepDaily, keepWeekly, keepMonthly int) []models.BackupLog {
	if keepDaily <= 0 && keepWeekly <= 0 && keepMonthly <= 0 {
		return nil
	}

	keepIDs := make(map[uint]bool)
	dailySeen := make(map[string]bool)
	weeklySeen := make(map[string]bool)
	monthlySeen := make(map[string]bool)

	for _, entry := range sorted {
		ts := entry.StartedAt.UTC()
		dayKey := ts.Format("2006-01-02")
		year, week := ts.ISOWeek()
		weekKey := fmt.Sprintf("%04d-W%02d", year, week)
		monthKey := ts.Format("2006-01")

		keep := false
		if keepDaily > 0 && !dailySeen[dayKey] && len(dailySeen) < keepDaily {
			dailySeen[dayKey] = true
			keep = true
		}
		if keepWeekly > 0 && !weeklySeen[weekKey] && len(weeklySeen) < keepWeekly {
			weeklySeen[weekKey] = true
			keep = true
		}
		if keepMonthly > 0 && !monthlySeen[monthKey] && len(monthlySeen) < keepMonthly {
			monthlySeen[monthKey] = true
			keep = true
		}
		if keep {
			keepIDs[entry.ID] = true
		}
	}

	var toDelete []models.BackupLog
	for _, entry := range sorted {
		if !keepIDs[entry.ID] {
			toDelete = append(toDelete, entry)
		}
	}
	return toDelete
}
