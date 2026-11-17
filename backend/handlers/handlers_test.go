package handlers

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/muecahit94/dockvault/config"
	dvcrypto "github.com/muecahit94/dockvault/crypto"
	"github.com/muecahit94/dockvault/docker"
	"github.com/muecahit94/dockvault/models"
	"github.com/muecahit94/dockvault/scheduler"
	"gorm.io/gorm"
)

func setupTestApp(t *testing.T) (*fiber.App, *gorm.DB, *config.Config, func()) {
	t.Helper()
	tmp := t.TempDir()
	cfg := &config.Config{
		Port:        "8080",
		DataDir:     filepath.Join(tmp, "data"),
		BackupDir:   filepath.Join(tmp, "backups"),
		HelperImage: "alpine:3.21",
		Version:     "test-1.0.0",
	}

	db, err := models.InitDB(cfg.DataDir, cfg.BackupDir)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	dockerSvc, err := docker.NewService(db, cfg.BackupDir, cfg.HelperImage)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	schedMgr := scheduler.NewManager(db, dockerSvc)
	app := NewApp(cfg, db, dockerSvc, schedMgr)

	cleanup := func() {
		schedMgr.Stop()
		_ = dockerSvc.Close()
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	}
	return app, db, cfg, cleanup
}

func doJSON(t *testing.T, app *fiber.App, method, path string, body any) (*http.Response, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test %s %s: %v", method, path, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, raw
}

func TestHealthAndSecurityHeaders(t *testing.T) {
	app, _, _, cleanup := setupTestApp(t)
	defer cleanup()

	resp, raw := doJSON(t, app, http.MethodGet, "/api/health", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing nosniff header")
	}
	if resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("missing X-Frame-Options header")
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal health: %v", err)
	}
	if payload["status"] != "ok" || payload["version"] != "test-1.0.0" {
		t.Fatalf("unexpected health payload: %s", raw)
	}
}

func TestStorageLifecycleAndSecretMasking(t *testing.T) {
	app, db, cfg, cleanup := setupTestApp(t)
	defer cleanup()

	// Create secondary local storage + S3 provider.
	local2Dir := filepath.Join(cfg.BackupDir, "secondary")
	resp, raw := doJSON(t, app, http.MethodPost, "/api/storage", map[string]any{
		"name":      "Secondary Disk",
		"type":      "local",
		"localPath": local2Dir,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create local storage failed (%d): %s", resp.StatusCode, raw)
	}

	resp, raw = doJSON(t, app, http.MethodPost, "/api/storage", map[string]any{
		"name":      "Offsite S3",
		"type":      "s3",
		"endpoint":  "127.0.0.1:19999",
		"region":    "eu-central-1",
		"bucket":    "backups",
		"accessKey": "AKIA_TEST",
		"secretKey": "SUPER_SECRET_KEY",
		"useSSL":    false,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create S3 storage failed (%d): %s", resp.StatusCode, raw)
	}
	if bytes.Contains(raw, []byte("SUPER_SECRET_KEY")) {
		t.Fatalf("secretKey leaked in API response: %s", raw)
	}

	var s3Created struct {
		Provider models.StorageProviderDTO `json:"provider"`
	}
	_ = json.Unmarshal(raw, &s3Created)
	if !s3Created.Provider.HasSecretKey {
		t.Fatalf("expected hasSecretKey=true")
	}
	if s3Created.Provider.UseSSL {
		t.Fatalf("expected useSSL=false to be persisted, got true")
	}

	// Updating without secretKey must preserve the stored secret in DB.
	resp, _ = doJSON(t, app, http.MethodPut, "/api/storage/"+itoa(s3Created.Provider.ID), map[string]any{
		"name":      "Offsite S3 Renamed",
		"type":      "s3",
		"endpoint":  "127.0.0.1:19999",
		"bucket":    "backups",
		"accessKey": "AKIA_TEST",
		"secretKey": "",
		"useSSL":    false,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update S3 provider failed: %d", resp.StatusCode)
	}
	var dbProvider models.StorageProvider
	db.First(&dbProvider, s3Created.Provider.ID)
	if dbProvider.SecretKey != "SUPER_SECRET_KEY" {
		t.Fatalf("expected existing secretKey preserved, got %q", dbProvider.SecretKey)
	}

	// Test default local storage probe succeeds.
	resp, _ = doJSON(t, app, http.MethodPost, "/api/storage/1/test", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected default local storage test to succeed, got %d", resp.StatusCode)
	}

	// Create Azure Blob Storage provider (Shared Key) and verify secret masking + defaults.
	resp, raw = doJSON(t, app, http.MethodPost, "/api/storage", map[string]any{
		"name":                     "Azure Offsite",
		"type":                     "azure",
		"azureAuthType":            "key",
		"azureAccountName":         "dockvaultbackups",
		"azureContainer":           "backups",
		"azureAccessTier":          "Cold",
		"azureAutoCreateContainer": true,
		"prefix":                   "homelab",
		"secretKey":                "dGVzdGtleXRlc3RrZXl0ZXN0a2V5dGVzdGtleXRlc3Q=",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create Azure storage failed (%d): %s", resp.StatusCode, raw)
	}
	if bytes.Contains(raw, []byte("dGVzdGtleXRlc3RrZXl0ZXN0a2V5dGVzdGtleXRlc3Q=")) {
		t.Fatalf("Azure secretKey leaked in API response: %s", raw)
	}
	var azCreated struct {
		Provider models.StorageProviderDTO `json:"provider"`
	}
	_ = json.Unmarshal(raw, &azCreated)
	if azCreated.Provider.AzureAccessTier != "Cold" || !azCreated.Provider.AzureAutoCreateContainer || !azCreated.Provider.HasSecretKey {
		t.Fatalf("unexpected Azure provider DTO: %+v", azCreated.Provider)
	}

	// Update Azure provider omitting secretKey -> preserves existing key & generates SAS link for backup log.
	resp, _ = doJSON(t, app, http.MethodPut, "/api/storage/"+itoa(azCreated.Provider.ID), map[string]any{
		"name":                     "Azure Offsite Updated",
		"type":                     "azure",
		"azureAuthType":            "key",
		"azureAccountName":         "dockvaultbackups",
		"azureContainer":           "backups",
		"azureAccessTier":          "Cool",
		"azureAutoCreateContainer": false,
		"secretKey":                "",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update Azure provider failed: %d", resp.StatusCode)
	}
	var azDB models.StorageProvider
	db.First(&azDB, azCreated.Provider.ID)
	if azDB.SecretKey != "dGVzdGtleXRlc3RrZXl0ZXN0a2V5dGVzdGtleXRlc3Q=" || azDB.AzureAutoCreateContainer {
		t.Fatalf("expected Azure secret preserved and autoCreate=false, got autoCreate=%v", azDB.AzureAutoCreateContainer)
	}

	// Verify POST /api/backups/:id/sas generates a valid 1-hour SAS URL for Azure backup logs.
	now := time.Now().UTC()
	azLog := models.BackupLog{
		Trigger:             "manual",
		TargetType:          "container",
		TargetID:            "pg",
		TargetName:          "pg",
		StorageProviderID:   azCreated.Provider.ID,
		StorageProviderName: azCreated.Provider.Name,
		StorageType:         "azure",
		AccessTier:          "Cool",
		Status:              "success",
		FileName:            "pg-20261117.tar.gz",
		StoragePath:         "homelab/pg-20261117.tar.gz",
		SizeBytes:           1024,
		StartedAt:           now,
		CompletedAt:         &now,
	}
	if err := db.Create(&azLog).Error; err != nil {
		t.Fatalf("create azure backup log: %v", err)
	}
	resp, raw = doJSON(t, app, http.MethodPost, "/api/backups/"+itoa(azLog.ID)+"/sas", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for Azure SAS link, got %d: %s", resp.StatusCode, raw)
	}
	var sasResp struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &sasResp); err != nil {
		t.Fatalf("unmarshal SAS response: %v", err)
	}
	if !bytes.HasPrefix([]byte(sasResp.URL), []byte("https://dockvaultbackups.blob.core.windows.net/backups/homelab/pg-20261117.tar.gz?")) {
		t.Fatalf("unexpected SAS link URL: %s", sasResp.URL)
	}

	// Verify Azure Service Principal / App Registration provider CRUD & secret masking.
	resp, raw = doJSON(t, app, http.MethodPost, "/api/storage", map[string]any{
		"name":             "Azure Entra SP",
		"type":             "azure",
		"azureAuthType":    "service_principal",
		"azureAccountName": "dockvaultbackups",
		"azureTenantId":    "11111111-1111-1111-1111-111111111111",
		"azureClientId":    "22222222-2222-2222-2222-222222222222",
		"azureContainer":   "backups",
		"azureAccessTier":  "Cool",
		"secretKey":        "SP_CLIENT_SECRET_VALUE",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create Azure SP storage failed (%d): %s", resp.StatusCode, raw)
	}
	if bytes.Contains(raw, []byte("SP_CLIENT_SECRET_VALUE")) {
		t.Fatalf("Azure SP client secret leaked in API response: %s", raw)
	}
	var spCreated struct {
		Provider models.StorageProviderDTO `json:"provider"`
	}
	_ = json.Unmarshal(raw, &spCreated)
	if spCreated.Provider.AzureAuthType != "service_principal" ||
		spCreated.Provider.AzureTenantID != "11111111-1111-1111-1111-111111111111" ||
		spCreated.Provider.AzureClientID != "22222222-2222-2222-2222-222222222222" ||
		!spCreated.Provider.HasSecretKey {
		t.Fatalf("unexpected Azure SP provider DTO: %+v", spCreated.Provider)
	}
}

func TestScheduleCRUDAndReferenceProtection(t *testing.T) {
	app, _, _, cleanup := setupTestApp(t)
	defer cleanup()

	// Cron preview valid & invalid.
	resp, _ := doJSON(t, app, http.MethodPost, "/api/schedules/preview", map[string]any{"cronExpr": "0 3 * * *"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected valid cron preview 200, got %d", resp.StatusCode)
	}
	resp, _ = doJSON(t, app, http.MethodPost, "/api/schedules/preview", map[string]any{"cronExpr": "bad-cron"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected invalid cron preview 400, got %d", resp.StatusCode)
	}

	// Create schedule referencing provider 1.
	resp, raw := doJSON(t, app, http.MethodPost, "/api/schedules", map[string]any{
		"name":              "Nightly DB",
		"targetType":        "container",
		"targetId":          "postgres",
		"targetName":        "postgres",
		"cronExpr":          "0 2 * * *",
		"storageProviderId": 1,
		"retentionCount":    5,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create schedule failed (%d): %s", resp.StatusCode, raw)
	}
	var created struct {
		Schedule struct {
			ID        uint       `json:"id"`
			Enabled   bool       `json:"enabled"`
			NextRunAt *time.Time `json:"nextRunAt"`
		} `json:"schedule"`
	}
	_ = json.Unmarshal(raw, &created)
	if !created.Schedule.Enabled || created.Schedule.NextRunAt == nil {
		t.Fatalf("expected enabled schedule with nextRunAt, got %s", raw)
	}

	// Toggle schedule off -> nextRunAt becomes nil.
	resp, raw = doJSON(t, app, http.MethodPost, "/api/schedules/"+itoa(created.Schedule.ID)+"/toggle", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("toggle failed: %d", resp.StatusCode)
	}

	// Delete schedule.
	resp, _ = doJSON(t, app, http.MethodDelete, "/api/schedules/"+itoa(created.Schedule.ID), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete schedule failed: %d", resp.StatusCode)
	}

	// Also verify explicit false booleans & 0 retention are persisted (not overwritten by GORM defaults).
	resp, raw = doJSON(t, app, http.MethodPost, "/api/schedules", map[string]any{
		"name":              "Paused Zero Retention",
		"targetType":        "container",
		"targetId":          "redis",
		"targetName":        "redis",
		"cronExpr":          "0 4 * * *",
		"storageProviderId": 1,
		"retentionCount":    0,
		"pauseContainer":    false,
		"enabled":           false,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create disabled schedule failed (%d): %s", resp.StatusCode, raw)
	}
	var disabledSched struct {
		Schedule struct {
			ID             uint `json:"id"`
			Enabled        bool `json:"enabled"`
			PauseContainer bool `json:"pauseContainer"`
			RetentionCount int  `json:"retentionCount"`
		} `json:"schedule"`
	}
	_ = json.Unmarshal(raw, &disabledSched)
	if disabledSched.Schedule.Enabled || disabledSched.Schedule.PauseContainer || disabledSched.Schedule.RetentionCount != 0 {
		t.Fatalf("expected false/0 values persisted, got %+v", disabledSched.Schedule)
	}
	_, _ = doJSON(t, app, http.MethodDelete, "/api/schedules/"+itoa(disabledSched.Schedule.ID), nil)

	// Deleting last remaining storage provider must be rejected.
	resp, _ = doJSON(t, app, http.MethodDelete, "/api/storage/1", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 when deleting last storage provider, got %d", resp.StatusCode)
	}
}

func TestBackupInspectVerifyDownloadAndDelete(t *testing.T) {
	app, db, cfg, cleanup := setupTestApp(t)
	defer cleanup()

	archivePath := filepath.Join(cfg.BackupDir, "demo-20261117.tar.gz")
	var tarBuf bytes.Buffer
	gzw := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gzw)
	fileData := []byte("hello-inspect")
	_ = tw.WriteHeader(&tar.Header{Name: "./vol_data/hello.txt", Mode: 0o644, Size: int64(len(fileData))})
	_, _ = tw.Write(fileData)
	_ = tw.Close()
	_ = gzw.Close()

	content := tarBuf.Bytes()
	sum := sha256.Sum256(content)
	if err := os.WriteFile(archivePath, content, 0o640); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	now := time.Now().UTC()
	entry := models.BackupLog{
		Trigger:             "manual",
		TargetType:          "container",
		TargetID:            "demo",
		TargetName:          "demo",
		StorageProviderID:   1,
		StorageProviderName: "Local Storage",
		StorageType:         "local",
		Status:              "success",
		FileName:            "demo-20261117.tar.gz",
		StoragePath:         archivePath,
		SizeBytes:           int64(len(content)),
		ChecksumSHA256:      hex.EncodeToString(sum[:]),
		StartedAt:           now,
		CompletedAt:         &now,
	}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatalf("create backup log: %v", err)
	}

	// Inspect archive entries.
	resp, raw := doJSON(t, app, http.MethodGet, "/api/backups/"+itoa(entry.ID)+"/inspect", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte("vol_data/hello.txt")) {
		t.Fatalf("expected inspect to list hello.txt, got %d: %s", resp.StatusCode, raw)
	}

	// Verify archive SHA-256 checksum.
	resp, raw = doJSON(t, app, http.MethodPost, "/api/backups/"+itoa(entry.ID)+"/verify", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte(`"match":true`)) {
		t.Fatalf("expected checksum match=true, got %d: %s", resp.StatusCode, raw)
	}

	// Download archive and verify exact bytes & headers.
	resp, raw = doJSON(t, app, http.MethodGet, "/api/backups/"+itoa(entry.ID)+"/download", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on download, got %d: %s", resp.StatusCode, raw)
	}
	if string(raw) != string(content) {
		t.Fatalf("downloaded content mismatch")
	}
	if resp.Header.Get("Content-Type") != "application/gzip" {
		t.Fatalf("unexpected content-type: %q", resp.Header.Get("Content-Type"))
	}

	// Delete backup log + file on disk.
	resp, _ = doJSON(t, app, http.MethodDelete, "/api/backups/"+itoa(entry.ID), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete backup failed: %d", resp.StatusCode)
	}
	if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
		t.Fatalf("expected archive file deleted from disk")
	}
}

func TestNotificationConfigAndWebhookTest(t *testing.T) {
	app, _, _, cleanup := setupTestApp(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	resp, raw := doJSON(t, app, http.MethodPut, "/api/notifications", map[string]any{
		"enabled":    true,
		"provider":   "discord",
		"webhookUrl": srv.URL,
		"onSuccess":  true,
		"onFailure":  true,
	})
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte(`"enabled":true`)) {
		t.Fatalf("update notifications failed (%d): %s", resp.StatusCode, raw)
	}

	resp, raw = doJSON(t, app, http.MethodPost, "/api/notifications/test", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte(`"ok":true`)) {
		t.Fatalf("test notification failed (%d): %s", resp.StatusCode, raw)
	}

	// Telegram bot token must be masked in responses and preserved when omitted on PUT.
	resp, raw = doJSON(t, app, http.MethodPut, "/api/notifications", map[string]any{
		"enabled":          true,
		"provider":         "telegram",
		"telegramBotToken": "123456:SECRET_BOT_TOKEN",
		"telegramChatId":   "-10012345",
		"telegramThreadId": "7",
		"onFailure":        true,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save telegram failed (%d): %s", resp.StatusCode, raw)
	}
	if bytes.Contains(raw, []byte("123456:SECRET_BOT_TOKEN")) || !bytes.Contains(raw, []byte(`"hasTelegramBotToken":true`)) {
		t.Fatalf("expected masked telegram token in response: %s", raw)
	}

	resp, _ = doJSON(t, app, http.MethodPut, "/api/notifications", map[string]any{
		"enabled":          true,
		"provider":         "telegram",
		"telegramBotToken": "",
		"telegramChatId":   "-10099999",
		"onFailure":        true,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update telegram without token failed: %d", resp.StatusCode)
	}
}

func TestMultipleNotificationChannelsCRUD(t *testing.T) {
	app, _, _, cleanup := setupTestApp(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Fresh DB starts with zero notification channels.
	resp, raw := doJSON(t, app, http.MethodGet, "/api/notifications", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte(`"notifications":[]`)) {
		t.Fatalf("expected empty notifications slice initially, got %d: %s", resp.StatusCode, raw)
	}

	// Create channel 1 (Discord).
	resp, raw = doJSON(t, app, http.MethodPost, "/api/notifications", map[string]any{
		"name":       "Ops Discord",
		"enabled":    true,
		"provider":   "discord",
		"webhookUrl": srv.URL + "/discord",
		"onSuccess":  false,
		"onFailure":  true,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating discord channel, got %d: %s", resp.StatusCode, raw)
	}
	var c1 struct {
		Notification models.NotificationConfigDTO `json:"notification"`
	}
	_ = json.Unmarshal(raw, &c1)

	// Create channel 2 (Telegram).
	resp, raw = doJSON(t, app, http.MethodPost, "/api/notifications", map[string]any{
		"name":             "Admin Telegram",
		"enabled":          true,
		"provider":         "telegram",
		"telegramBotToken": "999:TOP_SECRET",
		"telegramChatId":   "-100777",
		"onSuccess":        true,
		"onFailure":        true,
	})
	if resp.StatusCode != http.StatusCreated || bytes.Contains(raw, []byte("999:TOP_SECRET")) {
		t.Fatalf("unexpected telegram create response (%d): %s", resp.StatusCode, raw)
	}
	var c2 struct {
		Notification models.NotificationConfigDTO `json:"notification"`
	}
	_ = json.Unmarshal(raw, &c2)

	// List returns both channels.
	resp, raw = doJSON(t, app, http.MethodGet, "/api/notifications", nil)
	var listRes struct {
		Notifications []models.NotificationConfigDTO `json:"notifications"`
	}
	_ = json.Unmarshal(raw, &listRes)
	if resp.StatusCode != http.StatusOK || len(listRes.Notifications) != 2 {
		t.Fatalf("expected 2 channels, got %d: %s", len(listRes.Notifications), raw)
	}

	// Toggle channel 2 off and back on, preserving masked token.
	resp, raw = doJSON(t, app, http.MethodPost, "/api/notifications/"+itoa(c2.Notification.ID)+"/toggle", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte(`"enabled":false`)) {
		t.Fatalf("expected channel 2 disabled on toggle, got %d: %s", resp.StatusCode, raw)
	}
	resp, raw = doJSON(t, app, http.MethodPost, "/api/notifications/"+itoa(c2.Notification.ID)+"/toggle", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte(`"enabled":true`)) {
		t.Fatalf("expected channel 2 re-enabled on toggle, got %d: %s", resp.StatusCode, raw)
	}

	// Per-ID test works cleanly on channel 1.
	resp, raw = doJSON(t, app, http.MethodPost, "/api/notifications/"+itoa(c1.Notification.ID)+"/test", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte(`"ok":true`)) {
		t.Fatalf("expected per-id test ok, got %d: %s", resp.StatusCode, raw)
	}

	// Update channel 1.
	resp, raw = doJSON(t, app, http.MethodPut, "/api/notifications/"+itoa(c1.Notification.ID), map[string]any{
		"name":       "Ops Discord v2",
		"enabled":    true,
		"provider":   "discord",
		"webhookUrl": srv.URL + "/discord-v2",
		"onSuccess":  true,
		"onFailure":  true,
	})
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte(`"Ops Discord v2"`)) {
		t.Fatalf("expected updated channel name, got %d: %s", resp.StatusCode, raw)
	}

	// Delete channel 1 -> 1 remaining.
	resp, _ = doJSON(t, app, http.MethodDelete, "/api/notifications/"+itoa(c1.Notification.ID), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete channel 1 failed: %d", resp.StatusCode)
	}
	resp, raw = doJSON(t, app, http.MethodGet, "/api/notifications", nil)
	_ = json.Unmarshal(raw, &listRes)
	if len(listRes.Notifications) != 1 || listRes.Notifications[0].ID != c2.Notification.ID {
		t.Fatalf("expected only channel 2 remaining, got: %s", raw)
	}
}

func TestUIPreferencesLifecycle(t *testing.T) {
	app, _, _, cleanup := setupTestApp(t)
	defer cleanup()

	resp, raw := doJSON(t, app, http.MethodGet, "/api/settings/preferences", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte(`"theme":"dark"`)) {
		t.Fatalf("expected default dark theme, got %d: %s", resp.StatusCode, raw)
	}

	resp, raw = doJSON(t, app, http.MethodPut, "/api/settings/preferences", map[string]any{
		"theme":               "light",
		"accentColor":         "emerald",
		"density":             "compact",
		"instanceName":        "prod-eu",
		"defaultPage":         "containers",
		"defaultResourceTab":  "volumes",
		"showStoppedDefault":  false,
		"autoRefreshSeconds":  30,
		"timeFormat":          "12h",
		"defaultPauseBackups": false,
	})
	if resp.StatusCode != http.StatusOK ||
		!bytes.Contains(raw, []byte(`"theme":"light"`)) ||
		!bytes.Contains(raw, []byte(`"accentColor":"emerald"`)) ||
		!bytes.Contains(raw, []byte(`"instanceName":"prod-eu"`)) {
		t.Fatalf("unexpected preferences update response (%d): %s", resp.StatusCode, raw)
	}

	resp, raw = doJSON(t, app, http.MethodPost, "/api/settings/preferences/reset", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte(`"theme":"dark"`)) {
		t.Fatalf("reset preferences failed (%d): %s", resp.StatusCode, raw)
	}
}

func TestBuiltInAuthAndTrustedProxy(t *testing.T) {
	app, _, _, cleanup := setupTestApp(t)
	defer cleanup()

	// Auth starts disabled by default.
	resp, raw := doJSON(t, app, http.MethodGet, "/api/auth/status", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte(`"enabled":false`)) {
		t.Fatalf("expected auth disabled initially, got %d: %s", resp.StatusCode, raw)
	}

	// Setup admin user & enable auth.
	resp, _ = doJSON(t, app, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "secret-password",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("auth setup failed: %d", resp.StatusCode)
	}

	// Protected routes must reject unauthenticated callers now.
	resp, _ = doJSON(t, app, http.MethodGet, "/api/storage", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 when auth enabled without cookie, got %d", resp.StatusCode)
	}

	// Health stays public for Docker HEALTHCHECK.
	resp, _ = doJSON(t, app, http.MethodGet, "/api/health", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected /api/health to remain public, got %d", resp.StatusCode)
	}

	// Login and reuse session cookie.
	resp, _ = doJSON(t, app, http.MethodPost, "/api/auth/login", map[string]any{
		"username": "admin",
		"password": "secret-password",
	})
	if resp.StatusCode != http.StatusOK || len(resp.Cookies()) == 0 {
		t.Fatalf("login failed: %d", resp.StatusCode)
	}
	cookie := resp.Cookies()[0]

	req := httptest.NewRequest(http.MethodGet, "/api/storage", nil)
	req.AddCookie(cookie)
	authResp, err := app.Test(req, 5000)
	if err != nil || authResp.StatusCode != http.StatusOK {
		t.Fatalf("authenticated request failed: err=%v status=%v", err, authResp)
	}
	_ = authResp.Body.Close()

	// Configure trusted proxy header and verify proxy auth works.
	reqProxyCfg, _ := json.Marshal(map[string]any{
		"trustedProxyHeader": "Remote-User",
	})
	reqPut := httptest.NewRequest(http.MethodPut, "/api/auth/settings", bytes.NewReader(reqProxyCfg))
	reqPut.Header.Set("Content-Type", "application/json")
	reqPut.AddCookie(cookie)
	putResp, _ := app.Test(reqPut, 5000)
	_ = putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("update trustedProxyHeader failed: %d", putResp.StatusCode)
	}

	reqWithProxy := httptest.NewRequest(http.MethodGet, "/api/storage", nil)
	reqWithProxy.Header.Set("Remote-User", "muecahit")
	proxyResp, _ := app.Test(reqWithProxy, 5000)
	_ = proxyResp.Body.Close()
	if proxyResp.StatusCode != http.StatusOK {
		t.Fatalf("expected proxy header auth to succeed, got %d", proxyResp.StatusCode)
	}
}

func TestEncryptedArchiveSingleFilePinAndRemoteImport(t *testing.T) {
	app, db, cfg, cleanup := setupTestApp(t)
	defer cleanup()

	// Enable encryption on default local storage provider (ID 1).
	resp, raw := doJSON(t, app, http.MethodPut, "/api/storage/1", map[string]any{
		"name":                 "Local Encrypted",
		"type":                 "local",
		"localPath":            cfg.BackupDir,
		"encryptBackups":       true,
		"encryptionPassphrase": "vault-passphrase-123",
	})
	if resp.StatusCode != http.StatusOK || bytes.Contains(raw, []byte("vault-passphrase-123")) {
		t.Fatalf("update encrypted storage failed or leaked passphrase (%d): %s", resp.StatusCode, raw)
	}

	// Build a real .tar.gz and encrypt it into cfg.BackupDir.
	plainTmp := filepath.Join(t.TempDir(), "plain.tar.gz")
	var tarBuf bytes.Buffer
	gzw := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gzw)
	fileData := []byte("db-dump-line=ok")
	_ = tw.WriteHeader(&tar.Header{Name: "./vol_pg/pg.conf", Mode: 0o644, Size: int64(len(fileData))})
	_, _ = tw.Write(fileData)
	_ = tw.Close()
	_ = gzw.Close()
	_ = os.WriteFile(plainTmp, tarBuf.Bytes(), 0o600)

	encPath := filepath.Join(cfg.BackupDir, "pgstack-20261117-120000.tar.gz.enc")
	encSize, encSum, err := dvcrypto.EncryptFile(plainTmp, encPath, "vault-passphrase-123")
	if err != nil {
		t.Fatalf("EncryptFile: %v", err)
	}

	// Scan storage provider -> should discover 1 unimported encrypted object.
	resp, raw = doJSON(t, app, http.MethodPost, "/api/storage/1/scan", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte(`"inferredTarget":"pgstack"`)) || !bytes.Contains(raw, []byte(`"encrypted":true`)) {
		t.Fatalf("scan failed (%d): %s", resp.StatusCode, raw)
	}

	// Import it into History.
	resp, raw = doJSON(t, app, http.MethodPost, "/api/storage/1/import", map[string]any{
		"storagePaths": []string{encPath},
	})
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte(`"imported":1`)) {
		t.Fatalf("import failed (%d): %s", resp.StatusCode, raw)
	}

	var imported models.BackupLog
	db.First(&imported)
	// Store expected checksum/size as recorded on backup.
	db.Model(&imported).Updates(map[string]any{"size_bytes": encSize, "checksum_sha256": encSum})

	// Pin backup.
	resp, raw = doJSON(t, app, http.MethodPost, "/api/backups/"+itoa(imported.ID)+"/pin", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte(`"pinned":true`)) {
		t.Fatalf("pin toggle failed (%d): %s", resp.StatusCode, raw)
	}

	// Inspect encrypted archive -> transparently decrypted.
	resp, raw = doJSON(t, app, http.MethodGet, "/api/backups/"+itoa(imported.ID)+"/inspect", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte("vol_pg/pg.conf")) {
		t.Fatalf("inspect encrypted failed (%d): %s", resp.StatusCode, raw)
	}

	// Single-file extraction from encrypted archive.
	resp, raw = doJSON(t, app, http.MethodGet, "/api/backups/"+itoa(imported.ID)+"/file?path=./vol_pg/pg.conf", nil)
	if resp.StatusCode != http.StatusOK || string(raw) != "db-dump-line=ok" {
		t.Fatalf("single-file extract failed (%d): %q", resp.StatusCode, string(raw))
	}

	// Verify integrity + decryption check.
	resp, raw = doJSON(t, app, http.MethodPost, "/api/backups/"+itoa(imported.ID)+"/verify", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte(`"match":true`)) {
		t.Fatalf("verify encrypted failed (%d): %s", resp.StatusCode, raw)
	}

	// Download decrypted .tar.gz vs raw .tar.gz.enc.
	resp, raw = doJSON(t, app, http.MethodGet, "/api/backups/"+itoa(imported.ID)+"/download", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Equal(raw, tarBuf.Bytes()) {
		t.Fatalf("decrypted download mismatch (%d)", resp.StatusCode)
	}
	resp, raw = doJSON(t, app, http.MethodGet, "/api/backups/"+itoa(imported.ID)+"/download?raw=true", nil)
	if resp.StatusCode != http.StatusOK || !bytes.HasPrefix(raw, []byte("DVENC1")) {
		t.Fatalf("raw encrypted download mismatch (%d)", resp.StatusCode)
	}
}

func TestSecurityFrameAncestorsHeader(t *testing.T) {
	app, _, _, cleanup := setupTestApp(t)
	defer cleanup()

	resp, _ := doJSON(t, app, http.MethodGet, "/api/health", nil)
	if got := resp.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("expected X-Frame-Options=DENY by default, got %q", got)
	}

	tmp := t.TempDir()
	db, err := models.InitDB(filepath.Join(tmp, "data"), filepath.Join(tmp, "backups"))
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	dockerSvc, err := docker.NewService(db, filepath.Join(tmp, "backups"), "alpine:3.21")
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	schedMgr := scheduler.NewManager(db, dockerSvc)
	customApp := NewApp(&config.Config{
		Port:           "8080",
		DataDir:        filepath.Join(tmp, "data"),
		BackupDir:      filepath.Join(tmp, "backups"),
		HelperImage:    "alpine:3.21",
		FrameAncestors: "https://dashboard.example.com, https://home.example.com",
	}, db, dockerSvc, schedMgr)

	resp2, _ := doJSON(t, customApp, http.MethodGet, "/api/health", nil)
	if got := resp2.Header.Get("X-Frame-Options"); got != "" {
		t.Fatalf("expected X-Frame-Options to be omitted when FrameAncestors set, got %q", got)
	}
	wantCSP := "frame-ancestors 'self' https://dashboard.example.com https://home.example.com"
	if got := resp2.Header.Get("Content-Security-Policy"); got != wantCSP {
		t.Fatalf("expected CSP %q, got %q", wantCSP, got)
	}
}

func itoa(id uint) string {
	return string(bytes.TrimSpace([]byte(
		func() string {
			b, _ := json.Marshal(id)
			return string(b)
		}(),
	)))
}
