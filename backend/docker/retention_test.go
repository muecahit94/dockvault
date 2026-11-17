package docker

import (
	"testing"
	"time"

	"github.com/muecahit94/dockvault/models"
)

func TestSelectLogsToDeleteCountRespectsPinned(t *testing.T) {
	base := time.Date(2026, 11, 17, 2, 0, 0, 0, time.UTC)
	logs := []models.BackupLog{
		{ID: 1, Status: "success", StartedAt: base},
		{ID: 2, Status: "success", StartedAt: base.Add(-24 * time.Hour)},
		{ID: 3, Status: "success", StartedAt: base.Add(-48 * time.Hour), Pinned: true},
		{ID: 4, Status: "success", StartedAt: base.Add(-72 * time.Hour)},
	}

	del := SelectLogsToDelete(logs, RetentionPolicyConfig{Policy: "count", Count: 2})
	if len(del) != 1 || del[0].ID != 4 {
		t.Fatalf("expected only unpinned ID 4 deleted, got %+v", del)
	}
}

func TestSelectLogsToDeleteGFS(t *testing.T) {
	base := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	logs := []models.BackupLog{
		// Same day (keep newest ID 1, prune ID 2)
		{ID: 1, Status: "success", StartedAt: base},
		{ID: 2, Status: "success", StartedAt: base.Add(-4 * time.Hour)},
		// Previous day (keep for daily=2)
		{ID: 3, Status: "success", StartedAt: base.AddDate(0, 0, -1)},
		// 10 days ago (same month Nov, different week -> kept by weekly=2)
		{ID: 4, Status: "success", StartedAt: base.AddDate(0, 0, -10)},
		// 40 days ago (Oct -> kept by monthly=2)
		{ID: 5, Status: "success", StartedAt: base.AddDate(0, 0, -40)},
		// 90 days ago (Aug -> pruned since monthly=2 already satisfied by Nov and Oct)
		{ID: 6, Status: "success", StartedAt: base.AddDate(0, 0, -90)},
	}

	del := SelectLogsToDelete(logs, RetentionPolicyConfig{
		Policy:      "gfs",
		KeepDaily:   2,
		KeepWeekly:  2,
		KeepMonthly: 2,
	})
	if len(del) != 2 || del[0].ID != 2 || del[1].ID != 6 {
		t.Fatalf("expected IDs [2, 6] deleted, got %+v", del)
	}
}
