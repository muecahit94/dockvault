package scheduler

import (
	"path/filepath"
	"testing"

	"github.com/muecahit94/dockvault/models"
)

func TestValidateCron(t *testing.T) {
	for _, valid := range []string{"0 2 * * *", "*/15 * * * *", "@daily", "@every 6h"} {
		next, err := ValidateCron(valid)
		if err != nil {
			t.Fatalf("expected cron %q to be valid: %v", valid, err)
		}
		if next.IsZero() {
			t.Fatalf("expected non-zero next run for %q", valid)
		}
	}

	for _, invalid := range []string{"", "* * *", "not a cron", "99 99 * * *"} {
		if _, err := ValidateCron(invalid); err == nil {
			t.Fatalf("expected cron %q to fail validation", invalid)
		}
	}
}

func TestApplyGitOpsCandidates(t *testing.T) {
	tmp := t.TempDir()
	db, err := models.InitDB(filepath.Join(tmp, "data"), filepath.Join(tmp, "backups"))
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	mgr := NewManager(db, nil)
	defer mgr.Stop()

	res, err := mgr.applyGitOpsCandidates([]GitOpsCandidate{
		{
			TargetType:      "stack",
			TargetID:        "immich",
			TargetName:      "immich",
			CronExpr:        "0 3 * * *",
			ExcludePatterns: "**/thumbs/**",
			RetentionCount:  14,
			Compression:     "zstd",
			DBHotDump:       true,
		},
		{
			TargetType: "container",
			TargetID:   "bad-cron",
			TargetName: "bad-cron",
			CronExpr:   "invalid",
		},
	})
	if err != nil {
		t.Fatalf("applyGitOpsCandidates create: %v", err)
	}
	if res.Created != 1 || res.Skipped != 1 {
		t.Fatalf("expected 1 created, 1 skipped, got %+v", res)
	}

	// Second sync updates existing managed schedule without duplicating.
	res2, err := mgr.applyGitOpsCandidates([]GitOpsCandidate{
		{
			TargetType:      "stack",
			TargetID:        "immich",
			TargetName:      "immich",
			CronExpr:        "30 4 * * *",
			ExcludePatterns: "*.tmp",
			RetentionCount:  10,
			Compression:     "gzip",
			DBHotDump:       false,
		},
	})
	if err != nil {
		t.Fatalf("applyGitOpsCandidates update: %v", err)
	}
	if res2.Updated != 1 || res2.Created != 0 {
		t.Fatalf("expected 1 updated, 0 created, got %+v", res2)
	}
}
