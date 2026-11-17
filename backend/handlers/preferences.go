package handlers

import (
	"encoding/json"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/muecahit94/dockvault/models"
)

// UIPreferences defines user-configurable appearance and workspace defaults stored in SQLite.
type UIPreferences struct {
	Theme               string `json:"theme"`               // "dark", "light", "system"
	AccentColor         string `json:"accentColor"`         // "sky", "emerald", "violet", "amber", "rose"
	Density             string `json:"density"`             // "comfortable", "compact"
	InstanceName        string `json:"instanceName"`        // Optional host/node label (e.g. "prod-eu")
	DefaultPage         string `json:"defaultPage"`         // "dashboard", "containers", "schedules", "history"
	DefaultResourceTab  string `json:"defaultResourceTab"`  // "stacks", "containers", "volumes"
	ShowStoppedDefault  bool   `json:"showStoppedDefault"`  // Default state of "Show stopped" toggle
	AutoRefreshSeconds  int    `json:"autoRefreshSeconds"`  // 0, 10, 30, 60
	TimeFormat          string `json:"timeFormat"`          // "24h", "12h"
	DefaultPauseBackups bool   `json:"defaultPauseBackups"` // Default pause toggle in backup/schedule modals
}

func defaultUIPreferences() UIPreferences {
	return UIPreferences{
		Theme:               "dark",
		AccentColor:         "sky",
		Density:             "comfortable",
		InstanceName:        "",
		DefaultPage:         "dashboard",
		DefaultResourceTab:  "stacks",
		ShowStoppedDefault:  true,
		AutoRefreshSeconds:  0,
		TimeFormat:          "24h",
		DefaultPauseBackups: true,
	}
}

func (s *Server) loadUIPreferences() UIPreferences {
	prefs := defaultUIPreferences()
	raw := models.GetSetting(s.db, models.SettingUIPreferences, "")
	if raw == "" {
		return prefs
	}
	_ = json.Unmarshal([]byte(raw), &prefs)
	return normalizeUIPreferences(prefs)
}

func normalizeUIPreferences(in UIPreferences) UIPreferences {
	def := defaultUIPreferences()
	switch strings.ToLower(strings.TrimSpace(in.Theme)) {
	case "dark", "light", "system":
		in.Theme = strings.ToLower(strings.TrimSpace(in.Theme))
	default:
		in.Theme = def.Theme
	}
	switch strings.ToLower(strings.TrimSpace(in.AccentColor)) {
	case "sky", "emerald", "violet", "amber", "rose":
		in.AccentColor = strings.ToLower(strings.TrimSpace(in.AccentColor))
	default:
		in.AccentColor = def.AccentColor
	}
	switch strings.ToLower(strings.TrimSpace(in.Density)) {
	case "comfortable", "compact":
		in.Density = strings.ToLower(strings.TrimSpace(in.Density))
	default:
		in.Density = def.Density
	}
	switch strings.ToLower(strings.TrimSpace(in.DefaultPage)) {
	case "dashboard", "containers", "schedules", "history", "settings":
		in.DefaultPage = strings.ToLower(strings.TrimSpace(in.DefaultPage))
	default:
		in.DefaultPage = def.DefaultPage
	}
	switch strings.ToLower(strings.TrimSpace(in.DefaultResourceTab)) {
	case "stacks", "containers", "volumes":
		in.DefaultResourceTab = strings.ToLower(strings.TrimSpace(in.DefaultResourceTab))
	default:
		in.DefaultResourceTab = def.DefaultResourceTab
	}
	switch in.AutoRefreshSeconds {
	case 0, 10, 30, 60:
	default:
		in.AutoRefreshSeconds = def.AutoRefreshSeconds
	}
	switch strings.ToLower(strings.TrimSpace(in.TimeFormat)) {
	case "24h", "12h":
		in.TimeFormat = strings.ToLower(strings.TrimSpace(in.TimeFormat))
	default:
		in.TimeFormat = def.TimeFormat
	}
	in.InstanceName = strings.TrimSpace(in.InstanceName)
	if len(in.InstanceName) > 48 {
		in.InstanceName = in.InstanceName[:48]
	}
	return in
}

// GetUIPreferences returns persisted UI & workspace preferences.
func (s *Server) GetUIPreferences(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"preferences": s.loadUIPreferences(),
	})
}

// UpdateUIPreferences validates and saves UI & workspace preferences.
func (s *Server) UpdateUIPreferences(c *fiber.Ctx) error {
	var body UIPreferences
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON body"})
	}
	prefs := normalizeUIPreferences(body)
	raw, err := json.Marshal(prefs)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if err := models.SetSetting(s.db, models.SettingUIPreferences, string(raw)); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"preferences": prefs,
	})
}

// ResetUIPreferences restores UI preferences to factory defaults.
func (s *Server) ResetUIPreferences(c *fiber.Ctx) error {
	prefs := defaultUIPreferences()
	raw, _ := json.Marshal(prefs)
	if err := models.SetSetting(s.db, models.SettingUIPreferences, string(raw)); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"preferences": prefs,
	})
}
