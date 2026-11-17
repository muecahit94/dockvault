package handlers

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	dvcrypto "github.com/muecahit94/dockvault/crypto"
	"github.com/muecahit94/dockvault/docker"
	"github.com/muecahit94/dockvault/models"
	"github.com/muecahit94/dockvault/storage"
)

type runBackupPayload struct {
	TargetType         string   `json:"targetType"`
	TargetID           string   `json:"targetId"`
	StorageProviderID  uint     `json:"storageProviderId"`
	SecondaryStorageID *uint    `json:"secondaryStorageId"`
	PauseContainer     *bool    `json:"pauseContainer"`
	SelectedMounts     []string `json:"selectedMounts"`
	PreCommand         string   `json:"preCommand"`
	PostCommand        string   `json:"postCommand"`
}

// ListBackups returns historical backup logs ordered newest-first.
func (s *Server) ListBackups(c *fiber.Ctx) error {
	limit := c.QueryInt("limit", 100)
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	query := s.db.Order("started_at desc").Limit(limit)
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		query = query.Where("status = ?", status)
	}
	if target := strings.TrimSpace(c.Query("target")); target != "" {
		like := "%" + target + "%"
		query = query.Where("target_name LIKE ? OR file_name LIKE ?", like, like)
	}

	var logs []models.BackupLog
	if err := query.Find(&logs).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"backups": logs,
	})
}

// RunBackup validates the target and initiates an asynchronous safe backup.
func (s *Server) RunBackup(c *fiber.Ctx) error {
	var body runBackupPayload
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid JSON request body",
		})
	}

	pause := true
	if body.PauseContainer != nil {
		pause = *body.PauseContainer
	}

	logEntry, err := s.dockerSvc.StartBackupAsync(c.UserContext(), docker.BackupRequest{
		TargetType:         strings.TrimSpace(body.TargetType),
		TargetID:           strings.TrimSpace(body.TargetID),
		StorageProviderID:  body.StorageProviderID,
		SecondaryStorageID: body.SecondaryStorageID,
		PauseContainer:     pause,
		SelectedMounts:     body.SelectedMounts,
		PreCommand:         strings.TrimSpace(body.PreCommand),
		PostCommand:        strings.TrimSpace(body.PostCommand),
		Trigger:            "manual",
	})
	if err != nil {
		if errors.Is(err, docker.ErrBackupInProgress) {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"error": err.Error(),
			})
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"backup": logEntry,
	})
}

// TogglePinBackup toggles the Pinned flag to protect a backup from retention pruning.
func (s *Server) TogglePinBackup(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid backup id"})
	}
	var logEntry models.BackupLog
	if err := s.db.First(&logEntry, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "backup log not found"})
	}
	logEntry.Pinned = !logEntry.Pinned
	if err := s.db.Save(&logEntry).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"backup": logEntry})
}

// DownloadBackup streams the stored archive (decrypting on the fly unless ?raw=true).
func (s *Server) DownloadBackup(c *fiber.Ctx) error {
	logEntry, provider, driver, status, errMsg := s.openBackupContext(c.Params("id"))
	if status != 0 {
		return c.Status(status).JSON(fiber.Map{"error": errMsg})
	}

	rawMode := c.Query("raw") == "true"
	isEncrypted := logEntry.Encrypted || strings.HasSuffix(logEntry.StoragePath, ".enc")

	reader, size, err := s.openArchiveStream(c.UserContext(), logEntry, provider, driver, !rawMode)
	if err != nil {
		return c.Status(httpStatusForArchiveErr(err)).JSON(fiber.Map{"error": err.Error()})
	}

	safeName, err := storage.SafeFileName(logEntry.FileName)
	if err != nil {
		safeName = "dockvault-backup.tar.gz"
	}
	if !rawMode && isEncrypted {
		safeName = strings.TrimSuffix(safeName, ".enc")
	}

	if rawMode && isEncrypted {
		c.Set("Content-Type", "application/octet-stream")
	} else {
		c.Set("Content-Type", "application/gzip")
	}
	c.Set("Content-Disposition", `attachment; filename="`+safeName+`"`)
	if !isEncrypted || rawMode {
		if size > 0 {
			c.Set("Content-Length", strconv.FormatInt(size, 10))
		}
		return c.SendStream(io.NopCloser(readerWithAutoClose{r: reader}), int(size))
	}

	// Decrypted stream length differs from encrypted blob length; stream chunked without Content-Length.
	return c.SendStream(io.NopCloser(readerWithAutoClose{r: reader}))
}

type readerWithAutoClose struct {
	r io.ReadCloser
}

func (w readerWithAutoClose) Read(p []byte) (int, error) {
	n, err := w.r.Read(p)
	if err != nil {
		_ = w.r.Close()
	}
	return n, err
}

// InspectBackup lists the file and directory entries inside a stored .tar.gz (or encrypted) archive.
func (s *Server) InspectBackup(c *fiber.Ctx) error {
	logEntry, provider, driver, status, errMsg := s.openBackupContext(c.Params("id"))
	if status != 0 {
		return c.Status(status).JSON(fiber.Map{"error": errMsg})
	}

	reader, _, err := s.openArchiveStream(c.UserContext(), logEntry, provider, driver, true)
	if err != nil {
		return c.Status(httpStatusForArchiveErr(err)).JSON(fiber.Map{"error": err.Error()})
	}
	defer reader.Close()

	gz, err := gzip.NewReader(reader)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid gzip archive: " + err.Error()})
	}
	defer gz.Close()

	type archiveEntry struct {
		Name    string    `json:"name"`
		Size    int64     `json:"size"`
		IsDir   bool      `json:"isDir"`
		ModTime time.Time `json:"modTime"`
	}

	const maxEntries = 500
	tr := tar.NewReader(gz)
	entries := make([]archiveEntry, 0, 32)
	totalEntries := 0
	truncated := false

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "failed reading tar entries: " + err.Error()})
		}
		totalEntries++
		if len(entries) < maxEntries {
			entries = append(entries, archiveEntry{
				Name:    hdr.Name,
				Size:    hdr.Size,
				IsDir:   hdr.Typeflag == tar.TypeDir,
				ModTime: hdr.ModTime,
			})
		} else {
			truncated = true
		}
	}

	return c.JSON(fiber.Map{
		"entries":      entries,
		"totalEntries": totalEntries,
		"truncated":    truncated,
	})
}

// ExtractSingleFile streams a single file from inside a stored backup archive.
func (s *Server) ExtractSingleFile(c *fiber.Ctx) error {
	targetPath := strings.TrimSpace(c.Query("path"))
	if targetPath == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "path query parameter is required"})
	}
	cleanTarget := strings.TrimPrefix(path.Clean("/"+targetPath), "/")

	logEntry, provider, driver, status, errMsg := s.openBackupContext(c.Params("id"))
	if status != 0 {
		return c.Status(status).JSON(fiber.Map{"error": errMsg})
	}

	reader, _, err := s.openArchiveStream(c.UserContext(), logEntry, provider, driver, true)
	if err != nil {
		return c.Status(httpStatusForArchiveErr(err)).JSON(fiber.Map{"error": err.Error()})
	}

	gz, err := gzip.NewReader(reader)
	if err != nil {
		_ = reader.Close()
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid gzip archive: " + err.Error()})
	}

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			_ = gz.Close()
			_ = reader.Close()
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "read archive error: " + err.Error()})
		}
		cleanHdr := strings.TrimPrefix(path.Clean("/"+hdr.Name), "/")
		if cleanHdr == cleanTarget && hdr.Typeflag != tar.TypeDir {
			baseName, nameErr := storage.SafeFileName(path.Base(cleanHdr))
			if nameErr != nil {
				baseName = "extracted-file"
			}
			c.Set("Content-Type", "application/octet-stream")
			c.Set("Content-Disposition", `attachment; filename="`+baseName+`"`)
			if hdr.Size > 0 {
				c.Set("Content-Length", strconv.FormatInt(hdr.Size, 10))
			}
			return c.SendStream(multiCloseReader{r: tr, closers: []io.Closer{gz, reader}}, int(hdr.Size))
		}
	}

	_ = gz.Close()
	_ = reader.Close()
	return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "file not found inside archive"})
}

type multiCloseReader struct {
	r       io.Reader
	closers []io.Closer
}

func (m multiCloseReader) Read(p []byte) (int, error) {
	return m.r.Read(p)
}

func (m multiCloseReader) Close() error {
	for _, cl := range m.closers {
		_ = cl.Close()
	}
	return nil
}

// VerifyBackup recomputes the archive SHA-256 checksum from storage and compares it against the logged digest.
func (s *Server) VerifyBackup(c *fiber.Ctx) error {
	logEntry, provider, driver, status, errMsg := s.openBackupContext(c.Params("id"))
	if status != 0 {
		return c.Status(status).JSON(fiber.Map{"error": errMsg})
	}

	reader, _, err := driver.Open(c.UserContext(), logEntry.StoragePath)
	if err != nil {
		return c.Status(httpStatusForArchiveErr(err)).JSON(fiber.Map{"error": err.Error()})
	}
	defer reader.Close()

	hasher := sha256.New()
	bytesRead, err := io.Copy(hasher, reader)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	actual := hex.EncodeToString(hasher.Sum(nil))
	match := logEntry.ChecksumSHA256 == "" || actual == logEntry.ChecksumSHA256

	// For encrypted backups, also verify passphrase decryption succeeds when a checksum matched.
	if match && (logEntry.Encrypted || strings.HasSuffix(logEntry.StoragePath, ".enc")) {
		decReader, _, decErr := s.openArchiveStream(c.UserContext(), logEntry, provider, driver, true)
		if decErr != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "match": false, "error": decErr.Error()})
		}
		_, copyErr := io.Copy(io.Discard, decReader)
		_ = decReader.Close()
		if copyErr != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "match": false, "error": copyErr.Error()})
		}
	}

	return c.JSON(fiber.Map{
		"ok":        match,
		"match":     match,
		"expected":  logEntry.ChecksumSHA256,
		"actual":    actual,
		"bytesRead": bytesRead,
	})
}

// RestoreBackup extracts a backup archive back into its target Docker volumes/bind mounts.
func (s *Server) RestoreBackup(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid backup id"})
	}

	var logEntry models.BackupLog
	if err := s.db.First(&logEntry, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "backup log not found"})
	}

	var body struct {
		PauseContainer *bool `json:"pauseContainer"`
	}
	if len(c.Body()) > 0 {
		_ = c.BodyParser(&body)
	}
	pause := true
	if body.PauseContainer != nil {
		pause = *body.PauseContainer
	}

	res, err := s.dockerSvc.RestoreBackup(c.UserContext(), logEntry, pause)
	if err != nil {
		if errors.Is(err, docker.ErrBackupInProgress) || errors.Is(err, storage.ErrAzureArchiveTier) {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(res)
}

func (s *Server) openBackupContext(rawID string) (models.BackupLog, models.StorageProvider, storage.Driver, int, string) {
	id, err := strconv.ParseUint(rawID, 10, 64)
	if err != nil {
		return models.BackupLog{}, models.StorageProvider{}, nil, fiber.StatusBadRequest, "invalid backup id"
	}
	var logEntry models.BackupLog
	if err := s.db.First(&logEntry, id).Error; err != nil {
		return models.BackupLog{}, models.StorageProvider{}, nil, fiber.StatusNotFound, "backup log not found"
	}
	if logEntry.Status != "success" || logEntry.StoragePath == "" {
		return models.BackupLog{}, models.StorageProvider{}, nil, fiber.StatusBadRequest, "backup archive is not available"
	}
	var provider models.StorageProvider
	if err := s.db.First(&provider, logEntry.StorageProviderID).Error; err != nil {
		return models.BackupLog{}, models.StorageProvider{}, nil, fiber.StatusNotFound, "storage provider for this backup no longer exists"
	}
	driver, err := storage.NewDriver(provider, s.cfg.BackupDir)
	if err != nil {
		return models.BackupLog{}, models.StorageProvider{}, nil, fiber.StatusInternalServerError, err.Error()
	}
	return logEntry, provider, driver, 0, ""
}

func (s *Server) openArchiveStream(
	ctx context.Context,
	logEntry models.BackupLog,
	provider models.StorageProvider,
	driver storage.Driver,
	decrypt bool,
) (io.ReadCloser, int64, error) {
	reader, size, err := driver.Open(ctx, logEntry.StoragePath)
	if err != nil {
		return nil, 0, err
	}
	if decrypt && (logEntry.Encrypted || strings.HasSuffix(logEntry.StoragePath, ".enc")) {
		dec, decErr := dvcrypto.DecryptReader(reader, s.dockerSvc.ResolvePassphrase(provider))
		if decErr != nil {
			return nil, 0, decErr
		}
		return dec, 0, nil
	}
	return reader, size, nil
}

func httpStatusForArchiveErr(err error) int {
	if errors.Is(err, storage.ErrAzureArchiveTier) {
		return fiber.StatusConflict
	}
	if errors.Is(err, dvcrypto.ErrInvalidArchive) {
		return fiber.StatusBadRequest
	}
	return fiber.StatusNotFound
}

// CreateBackupSASLink generates a 1-hour read-only SAS download URL for an Azure backup.
func (s *Server) CreateBackupSASLink(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid backup id"})
	}

	var logEntry models.BackupLog
	if err := s.db.First(&logEntry, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "backup log not found"})
	}
	if logEntry.Status != "success" || logEntry.StoragePath == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "backup archive is not available"})
	}

	var provider models.StorageProvider
	if err := s.db.First(&provider, logEntry.StorageProviderID).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "storage provider no longer exists"})
	}

	driver, err := storage.NewDriver(provider, s.cfg.BackupDir)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	sasGen, ok := driver.(storage.SASURLGenerator)
	if !ok {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "SAS download links are only supported for Azure Blob Storage providers",
		})
	}

	ttl := time.Hour
	url, err := sasGen.GenerateSASURL(c.UserContext(), logEntry.StoragePath, ttl)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{
		"url":       url,
		"expiresAt": time.Now().UTC().Add(ttl),
	})
}

// DeleteBackup deletes a backup log entry and optionally removes its archive file.
func (s *Server) DeleteBackup(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid backup id",
		})
	}

	var logEntry models.BackupLog
	if err := s.db.First(&logEntry, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "backup log not found",
		})
	}

	deleteFile := c.Query("deleteFile", "true") != "false"
	if deleteFile && logEntry.Status == "success" && logEntry.StoragePath != "" {
		var provider models.StorageProvider
		if err := s.db.First(&provider, logEntry.StorageProviderID).Error; err == nil {
			if driver, err := storage.NewDriver(provider, s.cfg.BackupDir); err == nil {
				_ = driver.Delete(c.UserContext(), logEntry.StoragePath)
			}
		}
	}

	if err := s.db.Delete(&logEntry).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(fiber.Map{"deleted": true})
}
