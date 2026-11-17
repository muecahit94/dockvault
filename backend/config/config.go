package config

import (
	"os"
	"path/filepath"
	"strings"
)

// Version is injected at build time via -ldflags; defaults to "dev".
var Version = "dev"

// Config holds runtime configuration loaded from environment variables.
type Config struct {
	Port               string
	DataDir            string
	BackupDir          string
	HelperImage        string
	Version            string
	AuthUsername       string
	AuthPassword       string
	TrustedProxyHeader string
	EncryptionKey      string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	port := firstNonEmpty(os.Getenv("DOCKVAULT_PORT"), os.Getenv("PORT"), "8080")
	dataDir := firstNonEmpty(os.Getenv("DOCKVAULT_DATA_DIR"), "./data")
	backupDir := firstNonEmpty(os.Getenv("DOCKVAULT_BACKUP_DIR"), "./backups")
	helperImage := firstNonEmpty(os.Getenv("DOCKVAULT_HELPER_IMAGE"), "alpine:3.21")

	absData, err := filepath.Abs(dataDir)
	if err == nil {
		dataDir = absData
	}
	absBackup, err := filepath.Abs(backupDir)
	if err == nil {
		backupDir = absBackup
	}

	version := strings.TrimSpace(Version)
	if version == "" {
		version = "dev"
	}

	return &Config{
		Port:               port,
		DataDir:            dataDir,
		BackupDir:          backupDir,
		HelperImage:        helperImage,
		Version:            version,
		AuthUsername:       strings.TrimSpace(os.Getenv("DOCKVAULT_AUTH_USERNAME")),
		AuthPassword:       os.Getenv("DOCKVAULT_AUTH_PASSWORD"),
		TrustedProxyHeader: strings.TrimSpace(os.Getenv("DOCKVAULT_TRUSTED_PROXY_HEADER")),
		EncryptionKey:      os.Getenv("DOCKVAULT_ENCRYPTION_KEY"),
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
