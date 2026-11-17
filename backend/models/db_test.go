package models

import (
	"path/filepath"
	"testing"
	"time"
)

func TestInitDBSeedsDefaultAndRecoversStaleRunningLogs(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	backupDir := filepath.Join(tmp, "backups")

	db, err := InitDB(dataDir, backupDir)
	if err != nil {
		t.Fatalf("first InitDB failed: %v", err)
	}

	var providers []StorageProvider
	if err := db.Find(&providers).Error; err != nil {
		t.Fatalf("query providers: %v", err)
	}
	if len(providers) != 1 || providers[0].Type != "local" || !providers[0].IsDefault {
		t.Fatalf("unexpected seeded providers: %+v", providers)
	}

	// Simulate an interrupted running backup log prior to restart.
	stale := BackupLog{
		Trigger:           "manual",
		TargetType:        "container",
		TargetID:          "pg",
		TargetName:        "pg",
		StorageProviderID: providers[0].ID,
		Status:            "running",
		StartedAt:         time.Now().UTC().Add(-5 * time.Minute),
	}
	if err := db.Create(&stale).Error; err != nil {
		t.Fatalf("create stale log: %v", err)
	}

	sqlDB, _ := db.DB()
	_ = sqlDB.Close()

	// Re-open DB (idempotent seeding + stale log recovery).
	db2, err := InitDB(dataDir, backupDir)
	if err != nil {
		t.Fatalf("second InitDB failed: %v", err)
	}
	sqlDB2, _ := db2.DB()
	defer sqlDB2.Close()

	var count int64
	db2.Model(&StorageProvider{}).Count(&count)
	if count != 1 {
		t.Fatalf("expected 1 seeded provider after reopen, got %d", count)
	}

	var recovered BackupLog
	if err := db2.First(&recovered, stale.ID).Error; err != nil {
		t.Fatalf("load recovered log: %v", err)
	}
	if recovered.Status != "failed" || recovered.CompletedAt == nil {
		t.Fatalf("expected stale running log to be marked failed, got status=%q", recovered.Status)
	}
}
