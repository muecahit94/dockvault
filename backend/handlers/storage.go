package handlers

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/muecahit94/dockvault/models"
	"github.com/muecahit94/dockvault/storage"
)

type storagePayload struct {
	Name                     string `json:"name"`
	Type                     string `json:"type"`
	IsDefault                bool   `json:"isDefault"`
	LocalPath                string `json:"localPath"`
	Endpoint                 string `json:"endpoint"`
	Region                   string `json:"region"`
	Bucket                   string `json:"bucket"`
	Prefix                   string `json:"prefix"`
	AccessKey                string `json:"accessKey"`
	SecretKey                string `json:"secretKey"`
	UseSSL                   *bool  `json:"useSSL"`
	AzureAuthType            string `json:"azureAuthType"`
	AzureAccountName         string `json:"azureAccountName"`
	AzureTenantID            string `json:"azureTenantId"`
	AzureClientID            string `json:"azureClientId"`
	AzureContainer           string `json:"azureContainer"`
	AzureAccessTier          string `json:"azureAccessTier"`
	AzureAutoCreateContainer *bool  `json:"azureAutoCreateContainer"`
	Host                     string `json:"host"`
	Port                     *int   `json:"port"`
	Username                 string `json:"username"`
	Password                 string `json:"password"`
	SSHUseLocalKey           *bool  `json:"sshUseLocalKey"`
	SSHKeyPath               string `json:"sshKeyPath"`
	SSHPrivateKey            string `json:"sshPrivateKey"`
	SSHKeyPassphrase         string `json:"sshKeyPassphrase"`
	RemotePath               string `json:"remotePath"`
	SMBShare                 string `json:"smbShare"`
	SMBDomain                string `json:"smbDomain"`
	NFSExport                string `json:"nfsExport"`
	NFSUID                   *int   `json:"nfsUid"`
	NFSGID                   *int   `json:"nfsGid"`
	EncryptBackups           *bool  `json:"encryptBackups"`
	EncryptionPassphrase     string `json:"encryptionPassphrase"`
}

var archiveTimestampRe = regexp.MustCompile(`^(.+)-(\d{8}-\d{6})\.tar\.(?:gz|zst)(?:\.enc)?$`)

// ListStorageProviders returns all configured storage providers with masked secrets.
func (s *Server) ListStorageProviders(c *fiber.Ctx) error {
	var providers []models.StorageProvider
	if err := s.db.Order("is_default desc, id asc").Find(&providers).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	dtos := make([]models.StorageProviderDTO, 0, len(providers))
	for _, p := range providers {
		dtos = append(dtos, p.ToDTO())
	}
	return c.JSON(fiber.Map{"providers": dtos})
}

// CreateStorageProvider validates and persists a new local, S3, or Azure storage destination.
func (s *Server) CreateStorageProvider(c *fiber.Ctx) error {
	var body storagePayload
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON body"})
	}

	provider, err := s.buildStorageModel(body, nil)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	if provider.IsDefault {
		s.db.Model(&models.StorageProvider{}).Where("is_default = ?", true).Update("is_default", false)
	}

	if err := s.db.Create(&provider).Error; err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"provider": provider.ToDTO()})
}

// UpdateStorageProvider updates an existing storage destination while preserving secrets if omitted.
func (s *Server) UpdateStorageProvider(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid provider id"})
	}

	var existing models.StorageProvider
	if err := s.db.First(&existing, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "storage provider not found"})
	}

	var body storagePayload
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON body"})
	}

	updated, err := s.buildStorageModel(body, &existing)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	updated.ID = existing.ID
	updated.CreatedAt = existing.CreatedAt

	if updated.IsDefault {
		s.db.Model(&models.StorageProvider{}).Where("id != ?", updated.ID).Update("is_default", false)
	}

	if err := s.db.Save(&updated).Error; err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"provider": updated.ToDTO()})
}

// DeleteStorageProvider removes a storage destination if no active schedules reference it.
func (s *Server) DeleteStorageProvider(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid provider id"})
	}

	var totalCount int64
	s.db.Model(&models.StorageProvider{}).Count(&totalCount)
	if totalCount <= 1 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "cannot delete the last remaining storage provider",
		})
	}

	var scheduleRefs int64
	s.db.Model(&models.BackupSchedule{}).
		Where("storage_provider_id = ? OR secondary_storage_id = ?", id, id).
		Count(&scheduleRefs)
	if scheduleRefs > 0 {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{
			"error": "storage provider is referenced by existing backup schedules",
		})
	}

	var existing models.StorageProvider
	if err := s.db.First(&existing, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "storage provider not found"})
	}

	if err := s.db.Delete(&existing).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	if existing.IsDefault {
		var first models.StorageProvider
		if err := s.db.Order("id asc").First(&first).Error; err == nil {
			s.db.Model(&first).Update("is_default", true)
		}
	}

	return c.JSON(fiber.Map{"deleted": true})
}

// TestStorageProvider tests connectivity for a saved storage provider.
func (s *Server) TestStorageProvider(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid provider id"})
	}

	var provider models.StorageProvider
	if err := s.db.First(&provider, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "storage provider not found"})
	}

	return s.runDriverTest(c, provider)
}

// TestUnsavedStorageProvider tests storage credentials directly from the settings form before saving.
func (s *Server) TestUnsavedStorageProvider(c *fiber.Ctx) error {
	var body storagePayload
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": "invalid JSON body"})
	}
	if strings.TrimSpace(body.Name) == "" {
		body.Name = "Connection Test"
	}
	provider, err := s.buildStorageModel(body, nil)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	return s.runDriverTest(c, provider)
}

// ScanStorageProvider lists remote backup objects and marks which are already imported into History.
func (s *Server) ScanStorageProvider(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid provider id"})
	}

	var provider models.StorageProvider
	if err := s.db.First(&provider, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "storage provider not found"})
	}

	driver, err := storage.NewDriver(provider, s.cfg.BackupDir)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	lister, ok := driver.(storage.ObjectLister)
	if !ok {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "storage provider does not support scanning"})
	}

	objects, err := lister.List(c.UserContext())
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	var existingLogs []models.BackupLog
	s.db.Where("storage_provider_id = ?", provider.ID).Find(&existingLogs)
	knownPaths := make(map[string]bool, len(existingLogs))
	for _, l := range existingLogs {
		knownPaths[l.StoragePath] = true
	}

	type scannedObject struct {
		storage.RemoteObject
		InferredTarget  string `json:"inferredTarget"`
		AlreadyImported bool   `json:"alreadyImported"`
	}

	out := make([]scannedObject, 0, len(objects))
	for _, obj := range objects {
		target, _ := inferBackupMetadata(obj.Name, obj.LastModified)
		out = append(out, scannedObject{
			RemoteObject:    obj,
			InferredTarget:  target,
			AlreadyImported: knownPaths[obj.StoragePath],
		})
	}

	return c.JSON(fiber.Map{"objects": out})
}

// ImportStorageBackups imports selected remote backup files into BackupLog for disaster recovery.
func (s *Server) ImportStorageBackups(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid provider id"})
	}

	var provider models.StorageProvider
	if err := s.db.First(&provider, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "storage provider not found"})
	}

	var body struct {
		StoragePaths []string `json:"storagePaths"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON body"})
	}

	driver, err := storage.NewDriver(provider, s.cfg.BackupDir)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	lister, ok := driver.(storage.ObjectLister)
	if !ok {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "storage provider does not support scanning"})
	}

	objects, err := lister.List(c.UserContext())
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	wanted := make(map[string]bool, len(body.StoragePaths))
	for _, p := range body.StoragePaths {
		wanted[p] = true
	}

	var existingLogs []models.BackupLog
	s.db.Where("storage_provider_id = ?", provider.ID).Find(&existingLogs)
	knownPaths := make(map[string]bool, len(existingLogs))
	for _, l := range existingLogs {
		knownPaths[l.StoragePath] = true
	}

	imported := 0
	for _, obj := range objects {
		if len(wanted) > 0 && !wanted[obj.StoragePath] {
			continue
		}
		if knownPaths[obj.StoragePath] {
			continue
		}
		targetName, ts := inferBackupMetadata(obj.Name, obj.LastModified)
		entry := models.BackupLog{
			Trigger:             "imported",
			TargetType:          "stack",
			TargetID:            targetName,
			TargetName:          targetName,
			StorageProviderID:   provider.ID,
			StorageProviderName: provider.Name,
			StorageType:         provider.Type,
			Encrypted:           obj.Encrypted,
			Status:              "success",
			FileName:            obj.Name,
			StoragePath:         obj.StoragePath,
			SizeBytes:           obj.SizeBytes,
			StartedAt:           ts,
			CompletedAt:         &ts,
		}
		if err := s.db.Create(&entry).Error; err == nil {
			imported++
			knownPaths[obj.StoragePath] = true
		}
	}

	return c.JSON(fiber.Map{"imported": imported})
}

func inferBackupMetadata(fileName string, fallbackTime time.Time) (string, time.Time) {
	ts := fallbackTime.UTC()
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	if matches := archiveTimestampRe.FindStringSubmatch(fileName); len(matches) == 3 {
		if parsed, err := time.ParseInLocation("20060102-150405", matches[2], time.UTC); err == nil {
			ts = parsed
		}
		return matches[1], ts
	}
	cleaned := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(fileName, ".enc"), ".tar.gz"), ".tar.zst")
	if cleaned == "" {
		cleaned = "imported-backup"
	}
	return cleaned, ts
}

func (s *Server) runDriverTest(c *fiber.Ctx, provider models.StorageProvider) error {
	driver, err := storage.NewDriver(provider, s.cfg.BackupDir)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}

	if detailed, ok := driver.(storage.DetailedTester); ok {
		res, err := detailed.TestWithDetails(c.UserContext())
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
		return c.JSON(fiber.Map{
			"ok":               true,
			"message":          res.Message,
			"containerCreated": res.ContainerCreated,
			"objectCount":      res.ObjectCount,
			"totalBytes":       res.TotalBytes,
		})
	}

	if err := driver.Test(c.UserContext()); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (s *Server) buildStorageModel(body storagePayload, existing *models.StorageProvider) (models.StorageProvider, error) {
	name := strings.TrimSpace(body.Name)
	if name == "" {
		return models.StorageProvider{}, fiber.NewError(fiber.StatusBadRequest, "provider name is required")
	}

	pType := strings.TrimSpace(body.Type)
	if pType == "" && existing != nil {
		pType = existing.Type
	}
	switch pType {
	case "local", "s3", "azure", "ssh", "smb", "nfs":
	default:
		return models.StorageProvider{}, fiber.NewError(fiber.StatusBadRequest, "type must be 'local', 's3', 'azure', 'ssh', 'smb', or 'nfs'")
	}

	useSSL := true
	if existing != nil {
		useSSL = existing.UseSSL
	}
	if body.UseSSL != nil {
		useSSL = *body.UseSSL
	}

	autoCreate := false
	if existing != nil {
		autoCreate = existing.AzureAutoCreateContainer
	}
	if body.AzureAutoCreateContainer != nil {
		autoCreate = *body.AzureAutoCreateContainer
	}

	encryptBackups := false
	if existing != nil {
		encryptBackups = existing.EncryptBackups
	}
	if body.EncryptBackups != nil {
		encryptBackups = *body.EncryptBackups
	}

	port := 0
	nfsUID := 0
	nfsGID := 0
	sshUseLocalKey := false
	if existing != nil {
		port = existing.Port
		nfsUID = existing.NFSUID
		nfsGID = existing.NFSGID
		sshUseLocalKey = existing.SSHUseLocalKey
	}
	if body.Port != nil {
		port = *body.Port
	}
	if body.NFSUID != nil {
		nfsUID = *body.NFSUID
	}
	if body.NFSGID != nil {
		nfsGID = *body.NFSGID
	}
	if body.SSHUseLocalKey != nil {
		sshUseLocalKey = *body.SSHUseLocalKey
	}

	p := models.StorageProvider{
		Name:                     name,
		Type:                     pType,
		IsDefault:                body.IsDefault,
		LocalPath:                strings.TrimSpace(body.LocalPath),
		Endpoint:                 strings.TrimSpace(body.Endpoint),
		Region:                   strings.TrimSpace(body.Region),
		Bucket:                   strings.TrimSpace(body.Bucket),
		Prefix:                   strings.TrimSpace(body.Prefix),
		AccessKey:                strings.TrimSpace(body.AccessKey),
		SecretKey:                strings.TrimSpace(body.SecretKey),
		UseSSL:                   useSSL,
		AzureAuthType:            strings.ToLower(strings.TrimSpace(body.AzureAuthType)),
		AzureAccountName:         strings.TrimSpace(body.AzureAccountName),
		AzureTenantID:            strings.TrimSpace(body.AzureTenantID),
		AzureClientID:            strings.TrimSpace(body.AzureClientID),
		AzureContainer:           strings.TrimSpace(body.AzureContainer),
		AzureAccessTier:          strings.TrimSpace(body.AzureAccessTier),
		AzureAutoCreateContainer: autoCreate,
		Host:                     strings.TrimSpace(body.Host),
		Port:                     port,
		Username:                 strings.TrimSpace(body.Username),
		Password:                 body.Password,
		SSHUseLocalKey:           sshUseLocalKey,
		SSHKeyPath:               strings.TrimSpace(body.SSHKeyPath),
		SSHPrivateKey:            strings.TrimSpace(body.SSHPrivateKey),
		SSHKeyPassphrase:         body.SSHKeyPassphrase,
		RemotePath:               strings.TrimSpace(body.RemotePath),
		SMBShare:                 strings.TrimSpace(body.SMBShare),
		SMBDomain:                strings.TrimSpace(body.SMBDomain),
		NFSExport:                strings.TrimSpace(body.NFSExport),
		NFSUID:                   nfsUID,
		NFSGID:                   nfsGID,
		EncryptBackups:           encryptBackups,
		EncryptionPassphrase:     body.EncryptionPassphrase,
	}

	if existing != nil {
		if p.SecretKey == "" {
			p.SecretKey = existing.SecretKey
		}
		if p.Password == "" {
			p.Password = existing.Password
		}
		if p.SSHPrivateKey == "" {
			p.SSHPrivateKey = existing.SSHPrivateKey
		}
		if p.SSHKeyPassphrase == "" {
			p.SSHKeyPassphrase = existing.SSHKeyPassphrase
		}
		if p.EncryptionPassphrase == "" {
			p.EncryptionPassphrase = existing.EncryptionPassphrase
		}
	}

	if p.EncryptBackups && strings.TrimSpace(p.EncryptionPassphrase) == "" && strings.TrimSpace(s.cfg.EncryptionKey) == "" {
		return models.StorageProvider{}, fiber.NewError(fiber.StatusBadRequest, "encryption passphrase is required when archive encryption is enabled")
	}

	switch p.Type {
	case "local":
		if p.LocalPath == "" {
			p.LocalPath = s.cfg.BackupDir
		}
		return p, nil

	case "s3":
		if p.Endpoint == "" || p.Bucket == "" || p.AccessKey == "" || p.SecretKey == "" {
			return models.StorageProvider{}, fiber.NewError(fiber.StatusBadRequest, "S3 endpoint, bucket, accessKey, and secretKey are required")
		}
		return p, nil

	case "azure":
		if p.AzureAuthType == "" {
			p.AzureAuthType = "key"
		}
		if p.AzureContainer == "" {
			p.AzureContainer = p.Bucket
		}
		if p.AzureAccessTier == "" {
			p.AzureAccessTier = "Cool"
		}
		if _, err := storage.NewAzureDriver(p); err != nil {
			return models.StorageProvider{}, fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		return p, nil

	case "ssh":
		if _, err := storage.NewSSHDriver(p); err != nil {
			return models.StorageProvider{}, fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		return p, nil

	case "smb":
		if _, err := storage.NewSMBDriver(p); err != nil {
			return models.StorageProvider{}, fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		return p, nil

	case "nfs":
		if _, err := storage.NewNFSDriver(p); err != nil {
			return models.StorageProvider{}, fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		return p, nil
	}

	return p, nil
}
