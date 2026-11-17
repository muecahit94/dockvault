package handlers

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/muecahit94/dockvault/models"
	"github.com/muecahit94/dockvault/notify"
)

type notificationPayload struct {
	ID               uint   `json:"id"`
	Name             string `json:"name"`
	Enabled          *bool  `json:"enabled"`
	Provider         string `json:"provider"`
	WebhookURL       string `json:"webhookUrl"`
	TelegramBotToken string `json:"telegramBotToken"`
	TelegramChatID   string `json:"telegramChatId"`
	TelegramThreadID string `json:"telegramThreadId"`
	OnSuccess        *bool  `json:"onSuccess"`
	OnFailure        *bool  `json:"onFailure"`
}

func defaultNotificationName(provider string) string {
	switch provider {
	case "telegram":
		return "Telegram Bot"
	case "discord":
		return "Discord Webhook"
	case "slack":
		return "Slack Webhook"
	case "ntfy":
		return "ntfy Topic"
	case "generic":
		return "JSON Webhook"
	default:
		return "Notification Channel"
	}
}

func validateNotificationCredentials(cfg models.NotificationConfig) string {
	if cfg.Provider == "telegram" {
		if cfg.TelegramBotToken == "" || cfg.TelegramChatID == "" {
			return "telegramBotToken and telegramChatId are required for Telegram alerts"
		}
		return ""
	}
	if !strings.HasPrefix(cfg.WebhookURL, "http://") && !strings.HasPrefix(cfg.WebhookURL, "https://") {
		return "webhookUrl must start with http:// or https://"
	}
	return ""
}

func buildNotificationConfig(existing *models.NotificationConfig, body notificationPayload, requireCredsAlways bool) (models.NotificationConfig, error) {
	provider := strings.ToLower(strings.TrimSpace(body.Provider))
	if provider == "" && existing != nil {
		provider = existing.Provider
	}
	if provider == "" {
		provider = "discord"
	}
	switch provider {
	case "discord", "slack", "ntfy", "telegram", "generic":
	default:
		return models.NotificationConfig{}, fiber.NewError(fiber.StatusBadRequest, "provider must be discord, slack, ntfy, telegram, or generic")
	}

	var cfg models.NotificationConfig
	if existing != nil {
		cfg = *existing
	} else {
		cfg.Enabled = true
		cfg.OnFailure = true
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		if existing != nil && strings.TrimSpace(existing.Name) != "" && existing.Provider == provider {
			name = existing.Name
		} else {
			name = defaultNotificationName(provider)
		}
	}

	enabled := cfg.Enabled
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	onSuccess := cfg.OnSuccess
	if body.OnSuccess != nil {
		onSuccess = *body.OnSuccess
	}
	onFailure := cfg.OnFailure
	if body.OnFailure != nil {
		onFailure = *body.OnFailure
	}

	webhookURL := strings.TrimSpace(body.WebhookURL)
	botToken := strings.TrimSpace(body.TelegramBotToken)
	if botToken == "" && existing != nil {
		botToken = existing.TelegramBotToken
	}
	chatID := strings.TrimSpace(body.TelegramChatID)
	threadID := strings.TrimSpace(body.TelegramThreadID)

	cfg.Name = name
	cfg.Enabled = enabled
	cfg.Provider = provider
	cfg.WebhookURL = webhookURL
	cfg.TelegramBotToken = botToken
	cfg.TelegramChatID = chatID
	cfg.TelegramThreadID = threadID
	cfg.OnSuccess = onSuccess
	cfg.OnFailure = onFailure

	if enabled || requireCredsAlways {
		if msg := validateNotificationCredentials(cfg); msg != "" {
			return models.NotificationConfig{}, fiber.NewError(fiber.StatusBadRequest, msg)
		}
	}

	return cfg, nil
}

// GetNotifications returns all configured notification channels with masked secrets.
func (s *Server) GetNotifications(c *fiber.Ctx) error {
	var list []models.NotificationConfig
	if err := s.db.Order("id asc").Find(&list).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	dtos := make([]models.NotificationConfigDTO, 0, len(list))
	for _, item := range list {
		dtos = append(dtos, item.ToDTO())
	}
	var first any
	if len(dtos) > 0 {
		first = dtos[0]
	}
	return c.JSON(fiber.Map{
		"notifications": dtos,
		"notification":  first,
	})
}

func respondNotificationErr(c *fiber.Ctx, err error) error {
	var fe *fiber.Error
	if errors.As(err, &fe) {
		return c.Status(fe.Code).JSON(fiber.Map{"error": fe.Message})
	}
	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
}

// CreateNotification adds a new notification channel.
func (s *Server) CreateNotification(c *fiber.Ctx) error {
	var body notificationPayload
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON body"})
	}

	cfg, err := buildNotificationConfig(nil, body, true)
	if err != nil {
		return respondNotificationErr(c, err)
	}

	if err := s.db.Create(&cfg).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"notification": cfg.ToDTO()})
}

// UpdateNotification updates a specific notification channel by ID, preserving TelegramBotToken when blank.
func (s *Server) UpdateNotification(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid notification id"})
	}
	var existing models.NotificationConfig
	if err := s.db.First(&existing, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "notification channel not found"})
	}

	var body notificationPayload
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON body"})
	}

	cfg, buildErr := buildNotificationConfig(&existing, body, true)
	if buildErr != nil {
		return respondNotificationErr(c, buildErr)
	}

	if err := s.db.Save(&cfg).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"notification": cfg.ToDTO()})
}

// ToggleNotification enables or disables an individual notification channel.
func (s *Server) ToggleNotification(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid notification id"})
	}
	var cfg models.NotificationConfig
	if err := s.db.First(&cfg, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "notification channel not found"})
	}

	nextEnabled := !cfg.Enabled
	if nextEnabled {
		if msg := validateNotificationCredentials(cfg); msg != "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": msg})
		}
	}
	cfg.Enabled = nextEnabled
	if err := s.db.Save(&cfg).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"notification": cfg.ToDTO()})
}

// DeleteNotification deletes a notification channel by ID.
func (s *Server) DeleteNotification(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid notification id"})
	}
	res := s.db.Delete(&models.NotificationConfig{}, id)
	if res.Error != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": res.Error.Error()})
	}
	if res.RowsAffected == 0 {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "notification channel not found"})
	}
	return c.JSON(fiber.Map{"deleted": true})
}

// UpdateNotifications upserts the primary notification channel for single-channel API compatibility.
func (s *Server) UpdateNotifications(c *fiber.Ctx) error {
	var body notificationPayload
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON body"})
	}

	var existing *models.NotificationConfig
	var found models.NotificationConfig
	if err := s.db.Order("id asc").First(&found).Error; err == nil {
		existing = &found
	}

	cfg, buildErr := buildNotificationConfig(existing, body, false)
	if buildErr != nil {
		return respondNotificationErr(c, buildErr)
	}

	if existing == nil {
		if err := s.db.Create(&cfg).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
	} else if err := s.db.Save(&cfg).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"notification": cfg.ToDTO()})
}

// TestNotificationByID sends a test alert for a saved notification channel by ID.
func (s *Server) TestNotificationByID(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid notification id"})
	}
	var saved models.NotificationConfig
	if err := s.db.First(&saved, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "notification channel not found"})
	}

	var body notificationPayload
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&body); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON body"})
		}
	}

	return s.dispatchTestNotification(c, &saved, body)
}

// TestNotifications dispatches a sample alert using the supplied payload or saved notification channel.
func (s *Server) TestNotifications(c *fiber.Ctx) error {
	var body notificationPayload
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&body); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON body"})
		}
	}

	var saved models.NotificationConfig
	var savedPtr *models.NotificationConfig
	if body.ID > 0 {
		if err := s.db.First(&saved, body.ID).Error; err == nil {
			savedPtr = &saved
		}
	} else if err := s.db.Order("id asc").First(&saved).Error; err == nil {
		savedPtr = &saved
	}

	return s.dispatchTestNotification(c, savedPtr, body)
}

func (s *Server) dispatchTestNotification(c *fiber.Ctx, saved *models.NotificationConfig, body notificationPayload) error {
	provider := strings.ToLower(strings.TrimSpace(body.Provider))
	if provider == "" && saved != nil {
		provider = saved.Provider
	}

	cfg := models.NotificationConfig{
		Enabled:          true,
		Provider:         provider,
		WebhookURL:       strings.TrimSpace(body.WebhookURL),
		TelegramBotToken: strings.TrimSpace(body.TelegramBotToken),
		TelegramChatID:   strings.TrimSpace(body.TelegramChatID),
		TelegramThreadID: strings.TrimSpace(body.TelegramThreadID),
	}

	if cfg.Provider == "telegram" {
		if saved != nil && saved.Provider == "telegram" {
			if cfg.TelegramBotToken == "" {
				cfg.TelegramBotToken = saved.TelegramBotToken
			}
			if cfg.TelegramChatID == "" {
				cfg.TelegramChatID = saved.TelegramChatID
			}
			if cfg.TelegramThreadID == "" {
				cfg.TelegramThreadID = saved.TelegramThreadID
			}
		} else if saved != nil && cfg.TelegramBotToken == "" {
			cfg.TelegramBotToken = saved.TelegramBotToken
		}
		if cfg.TelegramBotToken == "" || cfg.TelegramChatID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "telegramBotToken and telegramChatId are required to send a test alert"})
		}
	} else {
		if cfg.WebhookURL == "" && saved != nil {
			cfg.WebhookURL = saved.WebhookURL
		}
		if cfg.WebhookURL == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "webhookUrl is required to send a test alert"})
		}
	}

	if err := notify.SendTest(c.UserContext(), cfg); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true})
}
