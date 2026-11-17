package scheduler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/muecahit94/dockvault/models"
)

func TestCheckStaleSchedulesAlertsOncePerOverduePeriod(t *testing.T) {
	tmp := t.TempDir()
	db, err := models.InitDB(filepath.Join(tmp, "data"), filepath.Join(tmp, "backups"))
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	mgr := NewManager(db, nil)
	defer mgr.Stop()

	var hits atomic.Int32
	var lastEvent atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(raw, &payload)
		lastEvent.Store(payload["event"])
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	db.Create(&models.NotificationConfig{Name: "hook", Enabled: true, Provider: "generic", WebhookURL: srv.URL, OnFailure: true})

	t0 := time.Now().UTC().Truncate(time.Second)
	sched := models.BackupSchedule{
		Name: "db nightly", TargetType: "container", TargetID: "c1", TargetName: "postgres",
		CronExpr: "0 3 * * *", StorageProviderID: 1, Enabled: true, AlertAfterHours: 2,
		CreatedAt: t0.Add(-5 * time.Hour),
	}
	if err := db.Create(&sched).Error; err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	// Schedules without an alert window are ignored.
	db.Create(&models.BackupSchedule{
		Name: "no alerts", TargetType: "volume", TargetID: "v1", TargetName: "v1",
		CronExpr: "0 3 * * *", StorageProviderID: 1, Enabled: true, CreatedAt: t0.Add(-50 * time.Hour),
	})

	ctx := context.Background()
	if n := mgr.CheckStaleSchedules(ctx, t0); n != 1 || hits.Load() != 1 {
		t.Fatalf("expected one stale alert, got n=%d hits=%d", n, hits.Load())
	}
	if ev := lastEvent.Load(); ev != "schedule.stale" {
		t.Fatalf("expected schedule.stale event, got %v", ev)
	}
	if n := mgr.CheckStaleSchedules(ctx, t0.Add(30*time.Minute)); n != 0 || hits.Load() != 1 {
		t.Fatalf("stale alert must not repeat for the same overdue period, got n=%d hits=%d", n, hits.Load())
	}

	// A new success resets the window; staleness is re-detected only after AlertAfterHours elapse again.
	done := t0.Add(time.Hour)
	sid := sched.ID
	db.Create(&models.BackupLog{
		ScheduleID: &sid, Trigger: "scheduled", TargetType: "container", TargetID: "c1", TargetName: "postgres",
		StorageProviderID: 1, Status: "success", StartedAt: done.Add(-time.Minute), CompletedAt: &done,
	})
	if n := mgr.CheckStaleSchedules(ctx, done.Add(time.Hour)); n != 0 {
		t.Fatalf("schedule with a recent success must not alert, got %d", n)
	}
	if n := mgr.CheckStaleSchedules(ctx, done.Add(3*time.Hour)); n != 1 || hits.Load() != 2 {
		t.Fatalf("expected a fresh alert after the next overdue period, got n=%d hits=%d", n, hits.Load())
	}
}
