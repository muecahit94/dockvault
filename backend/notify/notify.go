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
	var configs []models.NotificationConfig
	if err := db.Where("enabled = ?", true).Order("id asc").Find(&configs).Error; err != nil {
		return
	}
	for _, cfg := range configs {
		if cfg.Provider == "telegram" {
			if strings.TrimSpace(cfg.TelegramBotToken) == "" || strings.TrimSpace(cfg.TelegramChatID) == "" {
				continue
			}
		} else if strings.TrimSpace(cfg.WebhookURL) == "" {
			continue
		}
		if entry.Status == "success" && !cfg.OnSuccess {
			continue
		}
		if entry.Status == "failed" && !cfg.OnFailure {
			continue
		}
		_ = Send(ctx, cfg, entry)
	}
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

// Send formats and posts a notification alert according to the configured provider.
func Send(ctx context.Context, cfg models.NotificationConfig, entry *models.BackupLog) error {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		provider = "generic"
	}

	targetURL := strings.TrimSpace(cfg.WebhookURL)
	if provider != "telegram" && !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		return fmt.Errorf("webhook URL must start with http:// or https://")
	}

	statusEmoji := "✅"
	statusLabel := "Succeeded"
	color := 0x10b981 // emerald
	if entry.Status == "failed" {
		statusEmoji = "❌"
		statusLabel = "Failed"
		color = 0xf43f5e // rose
	}

	title := fmt.Sprintf("%s DockVault Backup %s: %s", statusEmoji, statusLabel, entry.TargetName)
	summary := fmt.Sprintf(
		"Target: %s (%s)\nStorage: %s (%s)\nTrigger: %s | Duration: %dms",
		entry.TargetName, entry.TargetType, entry.StorageProviderName, entry.StorageType, entry.Trigger, entry.DurationMs,
	)
	if entry.Status == "success" {
		summary += fmt.Sprintf("\nArchive: %s (%d bytes)", entry.FileName, entry.SizeBytes)
	} else if entry.ErrorMessage != "" {
		summary += fmt.Sprintf("\nError: %s", entry.ErrorMessage)
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
		if entry.Status == "failed" {
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
		body, _ := json.Marshal(map[string]any{
			"event":   "backup." + entry.Status,
			"title":   title,
			"message": summary,
			"backup":  entry,
		})
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
