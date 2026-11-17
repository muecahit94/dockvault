package models

import "time"

// User represents a local administrator account for built-in authentication.
type User struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"size:100;not null;uniqueIndex" json:"username"`
	PasswordHash string    `gorm:"size:255;not null" json:"-"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// AppSetting stores persistent key/value runtime settings such as session secrets and auth flags.
type AppSetting struct {
	Key       string    `gorm:"primaryKey;size:100" json:"key"`
	Value     string    `gorm:"type:text" json:"value"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// StorageProvider represents a configured backup destination (local filesystem, S3-compatible, or Azure Blob Storage).
type StorageProvider struct {
	ID                       uint      `gorm:"primaryKey" json:"id"`
	Name                     string    `gorm:"size:120;not null;uniqueIndex" json:"name"`
	Type                     string    `gorm:"size:20;not null" json:"type"` // "local", "s3", or "azure"
	IsDefault                bool      `json:"isDefault"`
	LocalPath                string    `gorm:"size:512" json:"localPath,omitempty"`
	Endpoint                 string    `gorm:"size:255" json:"endpoint,omitempty"`
	Region                   string    `gorm:"size:64" json:"region,omitempty"`
	Bucket                   string    `gorm:"size:128" json:"bucket,omitempty"`
	Prefix                   string    `gorm:"size:255" json:"prefix,omitempty"`
	AccessKey                string    `gorm:"size:255" json:"accessKey,omitempty"`
	SecretKey                string    `gorm:"size:2048" json:"-"`
	UseSSL                   bool      `json:"useSSL"`
	AzureAuthType            string    `gorm:"size:32" json:"azureAuthType,omitempty"` // "key", "sas", "connection_string", "service_principal"
	AzureAccountName         string    `gorm:"size:128" json:"azureAccountName,omitempty"`
	AzureTenantID            string    `gorm:"size:128" json:"azureTenantId,omitempty"`
	AzureClientID            string    `gorm:"size:128" json:"azureClientId,omitempty"`
	AzureContainer           string    `gorm:"size:128" json:"azureContainer,omitempty"`
	AzureAccessTier          string    `gorm:"size:32" json:"azureAccessTier,omitempty"` // "Hot", "Cool", "Cold", "Archive"
	AzureAutoCreateContainer bool      `json:"azureAutoCreateContainer"`
	EncryptBackups           bool      `json:"encryptBackups"`
	EncryptionPassphrase     string    `gorm:"size:512" json:"-"`
	CreatedAt                time.Time `json:"createdAt"`
	UpdatedAt                time.Time `json:"updatedAt"`
}

// StorageProviderDTO is the safe API representation that never exposes raw secrets.
type StorageProviderDTO struct {
	ID                       uint      `json:"id"`
	Name                     string    `json:"name"`
	Type                     string    `json:"type"`
	IsDefault                bool      `json:"isDefault"`
	LocalPath                string    `json:"localPath,omitempty"`
	Endpoint                 string    `json:"endpoint,omitempty"`
	Region                   string    `json:"region,omitempty"`
	Bucket                   string    `json:"bucket,omitempty"`
	Prefix                   string    `json:"prefix,omitempty"`
	AccessKey                string    `json:"accessKey,omitempty"`
	HasSecretKey             bool      `json:"hasSecretKey"`
	UseSSL                   bool      `json:"useSSL"`
	AzureAuthType            string    `json:"azureAuthType,omitempty"`
	AzureAccountName         string    `json:"azureAccountName,omitempty"`
	AzureTenantID            string    `json:"azureTenantId,omitempty"`
	AzureClientID            string    `json:"azureClientId,omitempty"`
	AzureContainer           string    `json:"azureContainer,omitempty"`
	AzureAccessTier          string    `json:"azureAccessTier,omitempty"`
	AzureAutoCreateContainer bool      `json:"azureAutoCreateContainer"`
	EncryptBackups           bool      `json:"encryptBackups"`
	HasEncryptionKey         bool      `json:"hasEncryptionKey"`
	CreatedAt                time.Time `json:"createdAt"`
	UpdatedAt                time.Time `json:"updatedAt"`
}

// ToDTO converts a StorageProvider into its API-safe representation.
func (s StorageProvider) ToDTO() StorageProviderDTO {
	return StorageProviderDTO{
		ID:                       s.ID,
		Name:                     s.Name,
		Type:                     s.Type,
		IsDefault:                s.IsDefault,
		LocalPath:                s.LocalPath,
		Endpoint:                 s.Endpoint,
		Region:                   s.Region,
		Bucket:                   s.Bucket,
		Prefix:                   s.Prefix,
		AccessKey:                s.AccessKey,
		HasSecretKey:             s.SecretKey != "",
		UseSSL:                   s.UseSSL,
		AzureAuthType:            s.AzureAuthType,
		AzureAccountName:         s.AzureAccountName,
		AzureTenantID:            s.AzureTenantID,
		AzureClientID:            s.AzureClientID,
		AzureContainer:           s.AzureContainer,
		AzureAccessTier:          s.AzureAccessTier,
		AzureAutoCreateContainer: s.AzureAutoCreateContainer,
		EncryptBackups:           s.EncryptBackups,
		HasEncryptionKey:         s.EncryptionPassphrase != "",
		CreatedAt:                s.CreatedAt,
		UpdatedAt:                s.UpdatedAt,
	}
}

// BackupSchedule defines a recurring cron backup job for a container, stack, or volume.
type BackupSchedule struct {
	ID                       uint             `gorm:"primaryKey" json:"id"`
	Name                     string           `gorm:"size:120;not null" json:"name"`
	TargetType               string           `gorm:"size:20;not null" json:"targetType"` // "container", "stack", or "volume"
	TargetID                 string           `gorm:"size:255;not null" json:"targetId"`
	TargetName               string           `gorm:"size:255;not null" json:"targetName"`
	CronExpr                 string           `gorm:"size:120;not null" json:"cronExpr"`
	StorageProviderID        uint             `gorm:"not null" json:"storageProviderId"`
	StorageProvider          *StorageProvider `gorm:"foreignKey:StorageProviderID" json:"storageProvider,omitempty"`
	SecondaryStorageID       *uint            `json:"secondaryStorageId,omitempty"`
	SecondaryStorageProvider *StorageProvider `gorm:"foreignKey:SecondaryStorageID" json:"secondaryStorageProvider,omitempty"`
	PauseContainer           bool             `json:"pauseContainer"`
	RetentionPolicy          string           `gorm:"size:20" json:"retentionPolicy"` // "count" or "gfs"
	RetentionCount           int              `json:"retentionCount"`
	KeepDaily                int              `json:"keepDaily"`
	KeepWeekly               int              `json:"keepWeekly"`
	KeepMonthly              int              `json:"keepMonthly"`
	PreCommand               string           `gorm:"size:512" json:"preCommand,omitempty"`
	PostCommand              string           `gorm:"size:512" json:"postCommand,omitempty"`
	Enabled                  bool             `json:"enabled"`
	LastRunAt                *time.Time       `json:"lastRunAt"`
	LastStatus               string           `gorm:"size:20" json:"lastStatus"`
	CreatedAt                time.Time        `json:"createdAt"`
	UpdatedAt                time.Time        `json:"updatedAt"`
}

// BackupLog records the execution and outcome of every manual or scheduled backup.
type BackupLog struct {
	ID                   uint       `gorm:"primaryKey" json:"id"`
	ScheduleID           *uint      `gorm:"index" json:"scheduleId"`
	Trigger              string     `gorm:"size:20;not null" json:"trigger"` // "manual", "scheduled", or "imported"
	TargetType           string     `gorm:"size:20;not null" json:"targetType"`
	TargetID             string     `gorm:"size:255;not null" json:"targetId"`
	TargetName           string     `gorm:"size:255;not null" json:"targetName"`
	StorageProviderID    uint       `gorm:"not null" json:"storageProviderId"`
	StorageProviderName  string     `gorm:"size:120" json:"storageProviderName"`
	StorageType          string     `gorm:"size:20" json:"storageType"`
	AccessTier           string     `gorm:"size:32" json:"accessTier,omitempty"`
	Encrypted            bool       `json:"encrypted"`
	Pinned               bool       `gorm:"index" json:"pinned"`
	Status               string     `gorm:"size:20;not null;index" json:"status"` // "running", "success", "failed"
	FileName             string     `gorm:"size:255" json:"fileName"`
	StoragePath          string     `gorm:"size:512" json:"storagePath"`
	SizeBytes            int64      `json:"sizeBytes"`
	ChecksumSHA256       string     `gorm:"size:64" json:"checksumSha256"`
	VolumeCount          int        `json:"volumeCount"`
	VolumesJSON          string     `gorm:"type:text" json:"volumesJson"`
	PausedContainersJSON string     `gorm:"type:text" json:"pausedContainersJson"`
	DurationMs           int64      `json:"durationMs"`
	ErrorMessage         string     `gorm:"type:text" json:"errorMessage"`
	StartedAt            time.Time  `gorm:"index" json:"startedAt"`
	CompletedAt          *time.Time `json:"completedAt"`
}

// NotificationConfig configures an alert delivery channel for backup completion and failures.
type NotificationConfig struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	Name             string    `gorm:"size:128" json:"name"`
	Enabled          bool      `json:"enabled"`
	Provider         string    `gorm:"size:32" json:"provider"` // "generic", "discord", "slack", "ntfy", "telegram"
	WebhookURL       string    `gorm:"size:1024" json:"webhookUrl"`
	TelegramBotToken string    `gorm:"size:255" json:"-"`
	TelegramChatID   string    `gorm:"size:128" json:"telegramChatId"`
	TelegramThreadID string    `gorm:"size:64" json:"telegramThreadId,omitempty"`
	OnSuccess        bool      `json:"onSuccess"`
	OnFailure        bool      `json:"onFailure"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// NotificationConfigDTO is the API-safe representation that masks TelegramBotToken.
type NotificationConfigDTO struct {
	ID                  uint      `json:"id"`
	Name                string    `json:"name"`
	Enabled             bool      `json:"enabled"`
	Provider            string    `json:"provider"`
	WebhookURL          string    `json:"webhookUrl"`
	HasTelegramBotToken bool      `json:"hasTelegramBotToken"`
	TelegramChatID      string    `json:"telegramChatId"`
	TelegramThreadID    string    `json:"telegramThreadId,omitempty"`
	OnSuccess           bool      `json:"onSuccess"`
	OnFailure           bool      `json:"onFailure"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

// ToDTO converts NotificationConfig into its API-safe representation.
func (n NotificationConfig) ToDTO() NotificationConfigDTO {
	return NotificationConfigDTO{
		ID:                  n.ID,
		Name:                n.Name,
		Enabled:             n.Enabled,
		Provider:            n.Provider,
		WebhookURL:          n.WebhookURL,
		HasTelegramBotToken: n.TelegramBotToken != "",
		TelegramChatID:      n.TelegramChatID,
		TelegramThreadID:    n.TelegramThreadID,
		OnSuccess:           n.OnSuccess,
		OnFailure:           n.OnFailure,
		UpdatedAt:           n.UpdatedAt,
	}
}
