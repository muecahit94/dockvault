package models

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/muecahit94/dockvault/auth"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	SettingSessionSecret      = "session_secret"
	SettingAuthEnabled        = "auth_enabled"
	SettingTrustedProxyHeader = "trusted_proxy_header"
	SettingUIPreferences      = "ui_preferences"
)

// InitDB opens the SQLite database, applies pragmas, runs migrations, and seeds defaults.
func InitDB(dataDir, defaultBackupDir string) (*gorm.DB, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	if err := os.MkdirAll(defaultBackupDir, 0o755); err != nil {
		return nil, fmt.Errorf("create backup directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, "dockvault.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	sqlDB, err := db.DB()
	if err == nil {
		// SQLite handles concurrent readers well in WAL mode with a single writer pool.
		sqlDB.SetMaxOpenConns(1)
		_, _ = sqlDB.Exec("PRAGMA journal_mode=WAL;")
		_, _ = sqlDB.Exec("PRAGMA foreign_keys=ON;")
		_, _ = sqlDB.Exec("PRAGMA busy_timeout=5000;")
	}

	if err := db.AutoMigrate(&User{}, &AppSetting{}, &StorageProvider{}, &BackupSchedule{}, &BackupLog{}, &RestoreLog{}, &NotificationConfig{}); err != nil {
		return nil, fmt.Errorf("auto-migrate schema: %w", err)
	}

	if err := seedDefaultStorage(db, defaultBackupDir); err != nil {
		return nil, fmt.Errorf("seed default storage: %w", err)
	}
	cleanupEmptyPlaceholderNotifications(db)
	if err := ensureSessionSecret(db); err != nil {
		return nil, fmt.Errorf("ensure session secret: %w", err)
	}

	// Mark any leftover "running" backup logs from a previous crash/restart as failed.
	now := time.Now().UTC()
	db.Model(&BackupLog{}).
		Where("status = ?", "running").
		Updates(map[string]any{
			"status":        "failed",
			"error_message": "Backup interrupted by server restart",
			"completed_at":  &now,
		})

	return db, nil
}

// GetSetting retrieves a setting value by key, returning defaultVal if absent.
func GetSetting(db *gorm.DB, key, defaultVal string) string {
	var s AppSetting
	res := db.Where("key = ?", key).Limit(1).Find(&s)
	if res.Error != nil || res.RowsAffected == 0 {
		return defaultVal
	}
	return s.Value
}

// SetSetting upserts a persistent key/value setting.
func SetSetting(db *gorm.DB, key, value string) error {
	return db.Save(&AppSetting{
		Key:       key,
		Value:     value,
		UpdatedAt: time.Now().UTC(),
	}).Error
}

func seedDefaultStorage(db *gorm.DB, defaultBackupDir string) error {
	var count int64
	if err := db.Model(&StorageProvider{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	defaultLocal := StorageProvider{
		Name:      "Local Storage",
		Type:      "local",
		IsDefault: true,
		LocalPath: defaultBackupDir,
	}
	return db.Create(&defaultLocal).Error
}

func cleanupEmptyPlaceholderNotifications(db *gorm.DB) {
	// Remove legacy unconfigured default placeholder rows from upgrades.
	db.Where(
		"enabled = ? AND COALESCE(name, '') = '' AND COALESCE(webhook_url, '') = '' AND COALESCE(telegram_bot_token, '') = '' AND COALESCE(telegram_chat_id, '') = ''",
		false,
	).Delete(&NotificationConfig{})
}

func ensureSessionSecret(db *gorm.DB) error {
	if existing := GetSetting(db, SettingSessionSecret, ""); existing != "" {
		return nil
	}
	secret, err := auth.GenerateSecret()
	if err != nil {
		return err
	}
	return SetSetting(db, SettingSessionSecret, secret)
}
