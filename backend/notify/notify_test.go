package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/muecahit94/dockvault/models"
)

func TestSendWebhookProviders(t *testing.T) {
	for _, provider := range []string{"discord", "slack", "ntfy", "generic"} {
		var gotBody []byte
		var gotTitle string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotTitle = r.Header.Get("Title")
			gotBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusNoContent)
		}))

		err := SendTest(context.Background(), models.NotificationConfig{
			Enabled:    true,
			Provider:   provider,
			WebhookURL: srv.URL,
		})
		srv.Close()
		if err != nil {
			t.Fatalf("provider %s failed: %v", provider, err)
		}

		if provider == "ntfy" {
			if gotTitle == "" || len(gotBody) == 0 {
				t.Fatalf("ntfy expected Title header and body")
			}
		} else {
			var parsed map[string]any
			if err := json.Unmarshal(gotBody, &parsed); err != nil {
				t.Fatalf("provider %s did not send valid JSON: %v", provider, err)
			}
		}
	}
}

func TestSendTelegramNotification(t *testing.T) {
	var gotPath string
	var gotPayload map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	origBase := telegramAPIBase
	telegramAPIBase = srv.URL
	defer func() { telegramAPIBase = origBase }()

	err := Send(context.Background(), models.NotificationConfig{
		Enabled:          true,
		Provider:         "telegram",
		TelegramBotToken: "12345:TEST_TOKEN",
		TelegramChatID:   "-100999888",
		TelegramThreadID: "42",
	}, &models.BackupLog{
		TargetType:          "stack",
		TargetName:          "<pg-stack>",
		StorageProviderName: "Local",
		StorageType:         "local",
		Status:              "failed",
		Trigger:             "scheduled",
		ErrorMessage:        "err <1&2>",
	})
	if err != nil {
		t.Fatalf("telegram Send failed: %v", err)
	}

	if gotPath != "/bot12345:TEST_TOKEN/sendMessage" {
		t.Fatalf("unexpected telegram endpoint path: %s", gotPath)
	}
	if gotPayload["chat_id"] != "-100999888" || gotPayload["message_thread_id"] != float64(42) {
		t.Fatalf("unexpected telegram payload: %+v", gotPayload)
	}
	text, _ := gotPayload["text"].(string)
	if !strings.Contains(text, "&lt;pg-stack&gt;") || !strings.Contains(text, "err &lt;1&amp;2&gt;") {
		t.Fatalf("expected HTML-escaped content in telegram message, got: %s", text)
	}
}

func TestDispatchBackupAlertMultipleChannels(t *testing.T) {
	tmp := t.TempDir()
	db, err := models.InitDB(filepath.Join(tmp, "data"), filepath.Join(tmp, "backups"))
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	var discordHits atomic.Int32
	var telegramHits atomic.Int32
	var disabledHits atomic.Int32
	var failOnlyHits atomic.Int32

	discordSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		discordHits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer discordSrv.Close()

	disabledSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		disabledHits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer disabledSrv.Close()

	failOnlySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		failOnlyHits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer failOnlySrv.Close()

	tgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		telegramHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer tgSrv.Close()

	origBase := telegramAPIBase
	telegramAPIBase = tgSrv.URL
	defer func() { telegramAPIBase = origBase }()

	channels := []models.NotificationConfig{
		{Name: "Ops Discord", Enabled: true, Provider: "discord", WebhookURL: discordSrv.URL, OnSuccess: true, OnFailure: true},
		{Name: "Ops Telegram", Enabled: true, Provider: "telegram", TelegramBotToken: "111:AAA", TelegramChatID: "-1001", OnSuccess: true, OnFailure: true},
		{Name: "Disabled Slack", Enabled: false, Provider: "slack", WebhookURL: disabledSrv.URL, OnSuccess: true, OnFailure: true},
		{Name: "Failure Pager", Enabled: true, Provider: "generic", WebhookURL: failOnlySrv.URL, OnSuccess: false, OnFailure: true},
	}
	for i := range channels {
		if err := db.Create(&channels[i]).Error; err != nil {
			t.Fatalf("create channel %d: %v", i, err)
		}
	}

	DispatchBackupAlert(context.Background(), db, &models.BackupLog{
		TargetType:          "stack",
		TargetName:          "media",
		StorageProviderName: "Local",
		StorageType:         "local",
		Status:              "success",
		Trigger:             "scheduled",
		FileName:            "media.tar.gz",
		SizeBytes:           1024,
	})

	if discordHits.Load() != 1 || telegramHits.Load() != 1 {
		t.Fatalf("expected both active channels to receive success alert, got discord=%d telegram=%d", discordHits.Load(), telegramHits.Load())
	}
	if disabledHits.Load() != 0 || failOnlyHits.Load() != 0 {
		t.Fatalf("expected disabled/failure-only channels skipped on success, got disabled=%d failOnly=%d", disabledHits.Load(), failOnlyHits.Load())
	}
}
