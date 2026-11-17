package handlers

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/muecahit94/dockvault/config"
	"github.com/muecahit94/dockvault/models"
	"gorm.io/gorm"
)

func createLocalArchiveBackup(t *testing.T, db *gorm.DB, cfg *config.Config, name string, files map[string]string, mod, startedAt time.Time) models.BackupLog {
	t.Helper()
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	for fname, body := range files {
		_ = tw.WriteHeader(&tar.Header{Name: "./data/" + fname, Mode: 0o644, Size: int64(len(body)), ModTime: mod})
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = gzw.Close()

	archivePath := filepath.Join(cfg.BackupDir, name)
	if err := os.WriteFile(archivePath, buf.Bytes(), 0o640); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	sum := sha256.Sum256(buf.Bytes())
	entry := models.BackupLog{
		Trigger: "manual", TargetType: "container", TargetID: "app", TargetName: "app",
		StorageProviderID: 1, StorageProviderName: "Local Storage", StorageType: "local",
		Status: "success", FileName: name, StoragePath: archivePath, SizeBytes: int64(buf.Len()),
		ChecksumSHA256: hex.EncodeToString(sum[:]), StartedAt: startedAt, CompletedAt: &startedAt,
	}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatalf("create backup log: %v", err)
	}
	return entry
}

func TestBackupNotesBulkActionsAndCompare(t *testing.T) {
	app, db, cfg, cleanup := setupTestApp(t)
	defer cleanup()

	t0 := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	older := createLocalArchiveBackup(t, db, cfg, "app-old.tar.gz", map[string]string{
		"keep.txt": "same", "edit.txt": "v1", "gone.txt": "bye",
	}, t0, t0)
	newer := createLocalArchiveBackup(t, db, cfg, "app-new.tar.gz", map[string]string{
		"keep.txt": "same", "edit.txt": "version-2", "new.txt": "hello",
	}, t0, t0.Add(time.Hour))

	// Notes.
	resp, raw := doJSON(t, app, http.MethodPut, "/api/backups/"+itoa(older.ID)+"/note", map[string]any{"note": "  before upgrade  "})
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte(`"note":"before upgrade"`)) {
		t.Fatalf("update note failed: %d %s", resp.StatusCode, raw)
	}
	resp, _ = doJSON(t, app, http.MethodPut, "/api/backups/"+itoa(older.ID)+"/note", map[string]any{"note": string(bytes.Repeat([]byte("x"), 501))})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for an overlong note, got %d", resp.StatusCode)
	}

	// Compare (arguments given newest-first must still diff old -> new).
	resp, raw = doJSON(t, app, http.MethodGet, "/api/backups/compare?base="+itoa(newer.ID)+"&target="+itoa(older.ID), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("compare failed: %d %s", resp.StatusCode, raw)
	}
	var cmp struct {
		Base    struct{ ID uint } `json:"base"`
		Summary map[string]int    `json:"summary"`
		Added   []struct{ Name string }
		Removed []struct{ Name string }
		Changed []struct{ Name string }
	}
	_ = json.Unmarshal(raw, &cmp)
	if cmp.Base.ID != older.ID ||
		cmp.Summary["added"] != 1 || cmp.Summary["removed"] != 1 || cmp.Summary["changed"] != 1 || cmp.Summary["unchanged"] != 1 ||
		cmp.Added[0].Name != "data/new.txt" || cmp.Removed[0].Name != "data/gone.txt" || cmp.Changed[0].Name != "data/edit.txt" {
		t.Fatalf("unexpected compare result: %s", raw)
	}

	// Bulk pin / verify / delete with per-item results.
	resp, raw = doJSON(t, app, http.MethodPost, "/api/backups/bulk", map[string]any{"action": "pin", "ids": []uint{older.ID, newer.ID, 9999}})
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte(`"succeeded":2`)) || !bytes.Contains(raw, []byte(`"failed":1`)) {
		t.Fatalf("bulk pin unexpected: %d %s", resp.StatusCode, raw)
	}
	var pinned int64
	db.Model(&models.BackupLog{}).Where("pinned = ?", true).Count(&pinned)
	if pinned != 2 {
		t.Fatalf("expected 2 pinned backups, got %d", pinned)
	}

	resp, raw = doJSON(t, app, http.MethodPost, "/api/backups/bulk", map[string]any{"action": "verify", "ids": []uint{older.ID, newer.ID}})
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte(`"succeeded":2`)) {
		t.Fatalf("bulk verify unexpected: %d %s", resp.StatusCode, raw)
	}

	resp, _ = doJSON(t, app, http.MethodPost, "/api/backups/bulk", map[string]any{"action": "explode", "ids": []uint{older.ID}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown bulk action, got %d", resp.StatusCode)
	}

	resp, raw = doJSON(t, app, http.MethodPost, "/api/backups/bulk", map[string]any{"action": "delete", "ids": []uint{older.ID, newer.ID}})
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte(`"succeeded":2`)) {
		t.Fatalf("bulk delete unexpected: %d %s", resp.StatusCode, raw)
	}
	if _, err := os.Stat(older.StoragePath); !os.IsNotExist(err) {
		t.Fatalf("expected archive removed by bulk delete")
	}
}

func TestScheduleCloneAndStaleAlertSettings(t *testing.T) {
	app, db, _, cleanup := setupTestApp(t)
	defer cleanup()

	resp, raw := doJSON(t, app, http.MethodPost, "/api/schedules", map[string]any{
		"targetType": "stack", "targetId": "immich", "targetName": "immich",
		"cronExpr": "0 3 * * *", "retentionCount": 5, "alertAfterHours": 26,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create schedule failed: %d %s", resp.StatusCode, raw)
	}
	var created struct {
		Schedule struct {
			ID              uint `json:"id"`
			AlertAfterHours int  `json:"alertAfterHours"`
			Stale           bool `json:"stale"`
		} `json:"schedule"`
	}
	_ = json.Unmarshal(raw, &created)
	if created.Schedule.AlertAfterHours != 26 || created.Schedule.Stale {
		t.Fatalf("unexpected alert settings on new schedule: %s", raw)
	}
	srcID := itoa(created.Schedule.ID)

	// Clone without targets: paused copy of the same target.
	resp, raw = doJSON(t, app, http.MethodPost, "/api/schedules/"+srcID+"/clone", nil)
	if resp.StatusCode != http.StatusCreated || !bytes.Contains(raw, []byte(`"name":"immich backup (copy)"`)) || !bytes.Contains(raw, []byte(`"enabled":false`)) {
		t.Fatalf("clone unexpected: %d %s", resp.StatusCode, raw)
	}

	// Apply to other targets: already-scheduled targets are skipped, duplicates collapse.
	resp, raw = doJSON(t, app, http.MethodPost, "/api/schedules/"+srcID+"/clone", map[string]any{
		"targets": []map[string]string{
			{"targetType": "stack", "targetId": "immich", "targetName": "immich"},
			{"targetType": "volume", "targetId": "pgdata", "targetName": "pgdata"},
			{"targetType": "volume", "targetId": "pgdata", "targetName": "pgdata"},
			{"targetType": "container", "targetId": "abc123", "targetName": "redis"},
		},
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("apply-to-targets failed: %d %s", resp.StatusCode, raw)
	}
	var applied struct {
		Schedules []struct {
			TargetName      string `json:"targetName"`
			RetentionCount  int    `json:"retentionCount"`
			AlertAfterHours int    `json:"alertAfterHours"`
			Enabled         bool   `json:"enabled"`
		} `json:"schedules"`
		Skipped []string `json:"skipped"`
	}
	_ = json.Unmarshal(raw, &applied)
	if len(applied.Schedules) != 2 || len(applied.Skipped) != 1 || applied.Skipped[0] != "immich" {
		t.Fatalf("unexpected apply result: %s", raw)
	}
	for _, sc := range applied.Schedules {
		if sc.RetentionCount != 5 || sc.AlertAfterHours != 26 || !sc.Enabled {
			t.Fatalf("policy not copied to %s: %+v", sc.TargetName, sc)
		}
	}

	resp, _ = doJSON(t, app, http.MethodPost, "/api/schedules/"+srcID+"/clone", map[string]any{
		"targets": []map[string]string{{"targetType": "bogus", "targetId": "x"}},
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid target type, got %d", resp.StatusCode)
	}

	// A schedule overdue past its window is flagged stale in the list.
	db.Model(&models.BackupSchedule{}).Where("id = ?", created.Schedule.ID).
		Update("created_at", time.Now().UTC().Add(-48*time.Hour))
	_, raw = doJSON(t, app, http.MethodGet, "/api/schedules", nil)
	if !bytes.Contains(raw, []byte(`"stale":true`)) {
		t.Fatalf("expected overdue schedule flagged stale: %s", raw)
	}
}

func TestLoginRateLimitsFailedAttempts(t *testing.T) {
	app, _, _, cleanup := setupTestApp(t)
	defer cleanup()

	resp, _ := doJSON(t, app, http.MethodPost, "/api/auth/setup", map[string]any{"username": "admin", "password": "correct-horse"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("auth setup failed: %d", resp.StatusCode)
	}

	// Successful logins do not count toward the limit.
	for i := 0; i < loginMaxFailedAttempts+2; i++ {
		if resp, _ := doJSON(t, app, http.MethodPost, "/api/auth/login", map[string]any{"username": "admin", "password": "correct-horse"}); resp.StatusCode != http.StatusOK {
			t.Fatalf("successful login %d unexpectedly rejected: %d", i, resp.StatusCode)
		}
	}
	for i := 0; i < loginMaxFailedAttempts; i++ {
		if resp, _ := doJSON(t, app, http.MethodPost, "/api/auth/login", map[string]any{"username": "admin", "password": "wrong"}); resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("failed attempt %d: expected 401, got %d", i, resp.StatusCode)
		}
	}
	resp, raw := doJSON(t, app, http.MethodPost, "/api/auth/login", map[string]any{"username": "admin", "password": "correct-horse"})
	if resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("expected 429 with Retry-After after %d failures, got %d: %s", loginMaxFailedAttempts, resp.StatusCode, raw)
	}
	// Other accounts from the same address are not locked out by failures against "admin".
	if resp, _ := doJSON(t, app, http.MethodPost, "/api/auth/login", map[string]any{"username": "someone-else", "password": "x"}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected separate limiter bucket per username, got %d", resp.StatusCode)
	}
}

func TestResolveClientIPHonorsOnlyTrustedProxies(t *testing.T) {
	trusted := parseTrustedProxies("172.16.0.0/12, 10.0.0.5 bogus")
	if len(trusted) != 2 {
		t.Fatalf("expected 2 parsed proxy ranges, got %d", len(trusted))
	}
	cases := []struct {
		name, remote, xff, want string
	}{
		{"direct client ignores header", "203.0.113.9", "1.2.3.4", "203.0.113.9"},
		{"trusted proxy without header", "172.18.0.2", "", "172.18.0.2"},
		{"trusted proxy forwards client", "172.18.0.2", "198.51.100.7", "198.51.100.7"},
		{"spoofed leftmost entry ignored", "172.18.0.2", "6.6.6.6, 198.51.100.7", "198.51.100.7"},
		{"chained trusted proxies skipped", "172.18.0.2", "198.51.100.7, 10.0.0.5", "198.51.100.7"},
		{"malformed hop stops at last good", "172.18.0.2", "garbage, 10.0.0.5", "10.0.0.5"},
	}
	for _, tc := range cases {
		if got := resolveClientIP(net.ParseIP(tc.remote), tc.xff, trusted); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
}
