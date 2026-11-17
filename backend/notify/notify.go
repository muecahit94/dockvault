package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/muecahit94/dockvault/models"
	"gorm.io/gorm"
)

var (
	httpClient      = &http.Client{Timeout: 10 * time.Second}
	telegramAPIBase = "https://api.telegram.org"
)

// DispatchBackupAlert reads all enabled NotificationConfig rows from DB and sends alerts for this outcome.
func DispatchBackupAlert(ctx context.Context, db *gorm.DB, entry *models.BackupLog) {
	if db == nil || entry == nil {
		return
	}
	for _, cfg := range activeChannels(db) {
		if entry.Status == "success" && !cfg.OnSuccess {
			continue
		}
		if entry.Status == "failed" && !cfg.OnFailure {
			continue
		}
		_ = Send(ctx, cfg, entry)
	}
}

// StaleSchedule describes a schedule without a recent successful backup.
type StaleSchedule struct {
	ScheduleID      uint       `json:"scheduleId"`
	Name            string     `json:"name"`
	TargetType      string     `json:"targetType"`
	TargetName      string     `json:"targetName"`
	CronExpr        string     `json:"cronExpr"`
	AlertAfterHours int        `json:"alertAfterHours"`
	LastSuccessAt   *time.Time `json:"lastSuccessAt"`
	LastStatus      string     `json:"lastStatus,omitempty"`
}

// DispatchStaleAlert notifies every active channel subscribed to failures about a stale schedule.
// It returns true when at least one channel accepted the alert.
func DispatchStaleAlert(ctx context.Context, db *gorm.DB, stale StaleSchedule) bool {
	if db == nil {
		return false
	}
	last := "never"
	if stale.LastSuccessAt != nil {
		last = stale.LastSuccessAt.UTC().Format(time.RFC3339)
	}
	summary := fmt.Sprintf(
		"Schedule: %s (cron %s)\nTarget: %s (%s)\nNo successful backup within %dh. Last success: %s",
		stale.Name, stale.CronExpr, stale.TargetName, stale.TargetType, stale.AlertAfterHours, last,
	)
	if stale.LastStatus != "" {
		summary += fmt.Sprintf("\nLast run status: %s", stale.LastStatus)
	}
	msg := Message{
		Event:   "schedule.stale",
		Title:   fmt.Sprintf("⚠️ DockVault Backup Overdue: %s", stale.TargetName),
		Summary: summary,
		Failed:  true,
		DataKey: "schedule",
		Data:    stale,
	}
	delivered := false
	for _, cfg := range activeChannels(db) {
		if !cfg.OnFailure {
			continue
		}
		if err := SendMessage(ctx, cfg, msg); err == nil {
			delivered = true
		}
	}
	return delivered
}

func activeChannels(db *gorm.DB) []models.NotificationConfig {
	var configs []models.NotificationConfig
	if err := db.Where("enabled = ?", true).Order("id asc").Find(&configs).Error; err != nil {
		return nil
	}
	out := configs[:0]
	for _, cfg := range configs {
		if cfg.Provider == "telegram" {
			if strings.TrimSpace(cfg.TelegramBotToken) == "" || strings.TrimSpace(cfg.TelegramChatID) == "" {
				continue
			}
		} else if strings.TrimSpace(cfg.WebhookURL) == "" {
			continue
		}
		out = append(out, cfg)
	}
	return out
}

// SendTest dispatches a sample notification to verify webhook configuration.
func SendTest(ctx context.Context, cfg models.NotificationConfig) error {
	now := time.Now().UTC()
	sample := &models.BackupLog{
		Trigger:             "manual",
		TargetType:          "stack",
		TargetName:          "dockvault-test-stack",
		StorageProviderName: "Local Storage",
		StorageType:         "local",
		Status:              "success",
		FileName:            "dockvault-test-stack-sample.tar.gz",
		SizeBytes:           1048576,
		VolumeCount:         2,
		DurationMs:          420,
		StartedAt:           now,
		CompletedAt:         &now,
	}
	return Send(ctx, cfg, sample)
}

// Message is a provider-agnostic alert rendered into Telegram/Discord/Slack/ntfy/generic payloads.
type Message struct {
	Event   string // generic webhook "event" field, e.g. "backup.failed" or "schedule.stale"
	Title   string
	Summary string
	Failed  bool
	DataKey string // generic webhook key holding Data (e.g. "backup", "schedule")
	Data    any
}

// Send formats and posts a backup outcome notification according to the configured provider.
func Send(ctx context.Context, cfg models.NotificationConfig, entry *models.BackupLog) error {
	statusEmoji := "✅"
	statusLabel := "Succeeded"
	if entry.Status == "failed" {
		statusEmoji = "❌"
		statusLabel = "Failed"
	}

	summary := fmt.Sprintf(
		"Target: %s (%s)\nStorage: %s (%s)\nTrigger: %s | Duration: %dms",
		entry.TargetName, entry.TargetType, entry.StorageProviderName, entry.StorageType, entry.Trigger, entry.DurationMs,
	)
	if entry.Status == "success" {
		summary += fmt.Sprintf("\nArchive: %s (%d bytes)", entry.FileName, entry.SizeBytes)
	} else if entry.ErrorMessage != "" {
		summary += fmt.Sprintf("\nError: %s", entry.ErrorMessage)
	}
	if note := strings.TrimSpace(entry.Note); note != "" {
		summary += fmt.Sprintf("\nNote: %s", note)
	}

	return SendMessage(ctx, cfg, Message{
		Event:   "backup." + entry.Status,
		Title:   fmt.Sprintf("%s DockVault Backup %s: %s", statusEmoji, statusLabel, entry.TargetName),
		Summary: summary,
		Failed:  entry.Status == "failed",
		DataKey: "backup",
		Data:    entry,
	})
}

// SendMessage posts a pre-rendered alert message to a single notification channel.
func SendMessage(ctx context.Context, cfg models.NotificationConfig, msg Message) error {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		provider = "generic"
	}

	targetURL := strings.TrimSpace(cfg.WebhookURL)
	if provider != "telegram" && !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		return fmt.Errorf("webhook URL must start with http:// or https://")
	}

	title := msg.Title
	summary := msg.Summary
	color := 0x10b981 // emerald
	if msg.Failed {
		color = 0xf43f5e // rose
	}

	var req *http.Request
	var err error

	switch provider {
	case "telegram":
		token := strings.TrimSpace(cfg.TelegramBotToken)
		chatID := strings.TrimSpace(cfg.TelegramChatID)
		if token == "" || chatID == "" {
			return fmt.Errorf("telegram botToken and chatId are required")
		}
		endpoint := fmt.Sprintf("%s/bot%s/sendMessage", strings.TrimRight(telegramAPIBase, "/"), token)
		text := fmt.Sprintf("<b>%s</b>\n<pre>%s</pre>", html.EscapeString(title), html.EscapeString(summary))
		payload := map[string]any{
			"chat_id":                  chatID,
			"text":                     text,
			"parse_mode":               "HTML",
			"disable_web_page_preview": true,
		}
		if threadStr := strings.TrimSpace(cfg.TelegramThreadID); threadStr != "" {
			if tid, convErr := strconv.Atoi(threadStr); convErr == nil && tid > 0 {
				payload["message_thread_id"] = tid
			}
		}
		body, _ := json.Marshal(payload)
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")

	case "ntfy":
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, targetURL, strings.NewReader(summary))
		if err != nil {
			return err
		}
		req.Header.Set("Title", title)
		if msg.Failed {
			req.Header.Set("Priority", "high")
			req.Header.Set("Tags", "rotating_light,package")
		} else {
			req.Header.Set("Tags", "white_check_mark,package")
		}

	case "discord":
		body, _ := json.Marshal(map[string]any{
			"embeds": []map[string]any{
				{
					"title":       title,
					"description": summary,
					"color":       color,
					"timestamp":   time.Now().UTC().Format(time.RFC3339),
				},
			},
		})
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")

	case "slack":
		body, _ := json.Marshal(map[string]any{
			"text": fmt.Sprintf("*%s*\n%s", title, summary),
		})
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")

	default:
		payload := map[string]any{
			"event":   msg.Event,
			"title":   title,
			"message": summary,
		}
		if msg.DataKey != "" {
			payload[msg.DataKey] = msg.Data
		}
		body, _ := json.Marshal(payload)
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send webhook: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}
