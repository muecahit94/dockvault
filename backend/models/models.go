package models

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

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

// StorageProvider represents a configured backup destination (local, S3, Azure, SSH/SFTP, SMB, or NFS).
type StorageProvider struct {
	ID                       uint      `gorm:"primaryKey" json:"id"`
	Name                     string    `gorm:"size:120;not null;uniqueIndex" json:"name"`
	Type                     string    `gorm:"size:20;not null" json:"type"` // "local", "s3", "azure", "ssh", "smb", or "nfs"
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
	Host                     string    `gorm:"size:255" json:"host,omitempty"`
	Port                     int       `json:"port,omitempty"`
	Username                 string    `gorm:"size:128" json:"username,omitempty"`
	Password                 string    `gorm:"type:text" json:"-"`
	SSHUseLocalKey           bool      `json:"sshUseLocalKey"`
	SSHKeyPath               string    `gorm:"size:512" json:"sshKeyPath,omitempty"`
	SSHPrivateKey            string    `gorm:"type:text" json:"-"`
	SSHKeyPassphrase         string    `gorm:"type:text" json:"-"`
	RemotePath               string    `gorm:"size:512" json:"remotePath,omitempty"`
	SMBShare                 string    `gorm:"size:128" json:"smbShare,omitempty"`
	SMBDomain                string    `gorm:"size:128" json:"smbDomain,omitempty"`
	NFSExport                string    `gorm:"size:255" json:"nfsExport,omitempty"`
	NFSUID                   int       `json:"nfsUid,omitempty"`
	NFSGID                   int       `json:"nfsGid,omitempty"`
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
	Host                     string    `json:"host,omitempty"`
	Port                     int       `json:"port,omitempty"`
	Username                 string    `json:"username,omitempty"`
	HasPassword              bool      `json:"hasPassword"`
	SSHUseLocalKey           bool      `json:"sshUseLocalKey"`
	SSHKeyPath               string    `json:"sshKeyPath,omitempty"`
	HasSSHPrivateKey         bool      `json:"hasSshPrivateKey"`
	HasSSHKeyPassphrase      bool      `json:"hasSshKeyPassphrase"`
	RemotePath               string    `json:"remotePath,omitempty"`
	SMBShare                 string    `json:"smbShare,omitempty"`
	SMBDomain                string    `json:"smbDomain,omitempty"`
	NFSExport                string    `json:"nfsExport,omitempty"`
	NFSUID                   int       `json:"nfsUid,omitempty"`
	NFSGID                   int       `json:"nfsGid,omitempty"`
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
		Host:                     s.Host,
		Port:                     s.Port,
		Username:                 s.Username,
		HasPassword:              s.Password != "",
		SSHUseLocalKey:           s.SSHUseLocalKey,
		SSHKeyPath:               s.SSHKeyPath,
		HasSSHPrivateKey:         s.SSHPrivateKey != "",
		HasSSHKeyPassphrase:      s.SSHKeyPassphrase != "",
		RemotePath:               s.RemotePath,
		SMBShare:                 s.SMBShare,
		SMBDomain:                s.SMBDomain,
		NFSExport:                s.NFSExport,
		NFSUID:                   s.NFSUID,
		NFSGID:                   s.NFSGID,
		EncryptBackups:           s.EncryptBackups,
		HasEncryptionKey:         s.EncryptionPassphrase != "",
		CreatedAt:                s.CreatedAt,
		UpdatedAt:                s.UpdatedAt,
	}
}

// ScheduleTarget identifies one Compose stack, container, or Docker volume included in a schedule.
type ScheduleTarget struct {
	TargetType string `json:"targetType"` // "container", "stack", or "volume"
	TargetID   string `json:"targetId"`
	TargetName string `json:"targetName"`
}

// BackupSchedule defines a recurring cron backup job for one or more containers, stacks, or volumes.
type BackupSchedule struct {
	ID                       uint             `gorm:"primaryKey" json:"id"`
	Name                     string           `gorm:"size:120;not null" json:"name"`
	TargetType               string           `gorm:"size:20;not null" json:"targetType"` // "container", "stack", "volume", or "multi"
	TargetID                 string           `gorm:"size:255;not null" json:"targetId"`
	TargetName               string           `gorm:"size:255;not null" json:"targetName"`
	TargetsJSON              string           `gorm:"type:text" json:"-"`
	Targets                  []ScheduleTarget `gorm:"-" json:"targets"`
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
	ExcludePatterns          string           `gorm:"type:text" json:"excludePatterns,omitempty"`
	Compression              string           `gorm:"size:16;default:'gzip'" json:"compression"`
	DBHotDump                bool             `json:"dbHotDump"`
	ManagedByLabel           bool             `json:"managedByLabel,omitempty"`
	Enabled                  bool             `json:"enabled"`
	AlertAfterHours          int              `json:"alertAfterHours"`          // 0 disables stale-backup alerts
	StaleAlertedAt           *time.Time       `json:"staleAlertedAt,omitempty"` // last stale alert sent; reset by a newer success
	LastRunAt                *time.Time       `json:"lastRunAt"`
	LastStatus               string           `gorm:"size:20" json:"lastStatus"`
	CreatedAt                time.Time        `json:"createdAt"`
	UpdatedAt                time.Time        `json:"updatedAt"`
}

// ResolvedTargets returns the normalized list of targets covered by this schedule.
func (s BackupSchedule) ResolvedTargets() []ScheduleTarget {
	candidates := s.Targets
	if len(candidates) == 0 && strings.TrimSpace(s.TargetsJSON) != "" {
		_ = json.Unmarshal([]byte(s.TargetsJSON), &candidates)
	}
	if len(candidates) == 0 && strings.TrimSpace(s.TargetID) != "" {
		tName := strings.TrimSpace(s.TargetName)
		if tName == "" {
			tName = strings.TrimSpace(s.TargetID)
		}
		candidates = []ScheduleTarget{{
			TargetType: strings.TrimSpace(s.TargetType),
			TargetID:   strings.TrimSpace(s.TargetID),
			TargetName: tName,
		}}
	}

	seen := make(map[string]bool, len(candidates))
	out := make([]ScheduleTarget, 0, len(candidates))
	for _, t := range candidates {
		tType := strings.TrimSpace(t.TargetType)
		tID := strings.TrimSpace(t.TargetID)
		if tType == "" || tID == "" {
			continue
		}
		tName := strings.TrimSpace(t.TargetName)
		if tName == "" {
			tName = tID
		}
		key := tType + ":" + tID
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ScheduleTarget{
			TargetType: tType,
			TargetID:   tID,
			TargetName: tName,
		})
	}
	return out
}

// SetTargets stores a normalized slice of schedule targets and updates legacy summary columns.
func (s *BackupSchedule) SetTargets(targets []ScheduleTarget) {
	s.Targets = nil
	s.TargetsJSON = ""
	if len(targets) == 0 {
		return
	}
	s.Targets = targets
	if raw, err := json.Marshal(targets); err == nil {
		s.TargetsJSON = string(raw)
	}
	first := targets[0]
	commonType := first.TargetType
	names := make([]string, 0, len(targets))
	for _, t := range targets {
		if t.TargetType != commonType {
			commonType = "multi"
		}
		names = append(names, t.TargetName)
	}
	s.TargetType = commonType
	s.TargetID = first.TargetID
	joined := strings.Join(names, ", ")
	if len(joined) > 250 {
		joined = fmt.Sprintf("%s (+%d more)", first.TargetName, len(targets)-1)
		if len(joined) > 250 {
			joined = joined[:250]
		}
	}
	s.TargetName = joined
}

// BeforeSave ensures TargetsJSON stays synchronized before database persistence.
func (s *BackupSchedule) BeforeSave(_ *gorm.DB) error {
	if resolved := s.ResolvedTargets(); len(resolved) > 0 {
		s.Targets = resolved
		if raw, err := json.Marshal(resolved); err == nil {
			s.TargetsJSON = string(raw)
		}
	}
	return nil
}

// AfterFind populates the Targets slice after loading a schedule from the database.
func (s *BackupSchedule) AfterFind(_ *gorm.DB) error {
	s.Targets = s.ResolvedTargets()
	return nil
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
	Compression          string     `gorm:"size:16" json:"compression,omitempty"`
	ExcludePatterns      string     `gorm:"type:text" json:"excludePatterns,omitempty"`
	Encrypted            bool       `json:"encrypted"`
	Pinned               bool       `gorm:"index" json:"pinned"`
	Note                 string     `gorm:"size:500" json:"note,omitempty"`
	Status               string     `gorm:"size:20;not null;index" json:"status"` // "running", "success", "failed"
	FileName             string     `gorm:"size:255" json:"fileName"`
	StoragePath          string     `gorm:"size:512" json:"storagePath"`
	SizeBytes            int64      `json:"sizeBytes"`
	ChecksumSHA256       string     `gorm:"size:64" json:"checksumSha256"`
	VolumeCount          int        `json:"volumeCount"`
	VolumesJSON          string     `gorm:"type:text" json:"volumesJson"`
	PausedContainersJSON string     `gorm:"type:text" json:"pausedContainersJson"`
	StepsJSON            string     `gorm:"type:text" json:"stepsJson,omitempty"`
	DurationMs           int64      `json:"durationMs"`
	ErrorMessage         string     `gorm:"type:text" json:"errorMessage"`
	StartedAt            time.Time  `gorm:"index" json:"startedAt"`
	CompletedAt          *time.Time `json:"completedAt"`
}

// RestoreLog records every archive restore / rollback operation and the exact backup version restored.
type RestoreLog struct {
	ID                 uint      `gorm:"primaryKey" json:"id"`
	BackupLogID        uint      `gorm:"index;not null" json:"backupLogId"`
	BackupFileName     string    `gorm:"size:255" json:"backupFileName"`
	BackupStartedAt    time.Time `json:"backupStartedAt"`
	BackupTrigger      string    `gorm:"size:20" json:"backupTrigger,omitempty"`
	BackupNote         string    `gorm:"size:500" json:"backupNote,omitempty"`
	TargetType         string    `gorm:"size:20;not null;index" json:"targetType"`
	TargetID           string    `gorm:"size:255;not null" json:"targetId"`
	TargetName         string    `gorm:"size:255;not null;index" json:"targetName"`
	Mode               string    `gorm:"size:20;not null" json:"mode"` // "inplace" or "staging"
	TargetVolume       string    `gorm:"size:255" json:"targetVolume,omitempty"`
	PreRestoreBackupID *uint     `json:"preRestoreBackupId,omitempty"`
	RestoredMounts     int       `json:"restoredMounts"`
	RestoredPaths      int       `json:"restoredPaths,omitempty"`
	Status             string    `gorm:"size:20;not null" json:"status"` // "success" or "failed"
	DurationMs         int64     `json:"durationMs"`
	ErrorMessage       string    `gorm:"type:text" json:"errorMessage,omitempty"`
	RestoredAt         time.Time `gorm:"index" json:"restoredAt"`
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
