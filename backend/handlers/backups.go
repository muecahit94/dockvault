package handlers

import (
	"archive/tar"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	"gorm.io/gorm"
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
	ExcludePatterns    string   `json:"excludePatterns"`
	Compression        string   `json:"compression"`
	DBHotDump          *bool    `json:"dbHotDump"`
	Note               string   `json:"note"`
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

	restoreState := computeRestoreState(s.db, 200)
	return c.JSON(fiber.Map{
		"backups":         logs,
		"restores":        restoreState.Restores,
		"activeRollbacks": restoreState.ActiveRollbacks,
	})
}

// ListRestores returns recorded restore / rollback events ordered newest-first alongside active rollback states.
func (s *Server) ListRestores(c *fiber.Ctx) error {
	limit := c.QueryInt("limit", 100)
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	restoreState := computeRestoreState(s.db, limit)
	return c.JSON(fiber.Map{
		"restores":        restoreState.Restores,
		"activeRollbacks": restoreState.ActiveRollbacks,
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
	dbHotDump := false
	if body.DBHotDump != nil {
		dbHotDump = *body.DBHotDump
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
		ExcludePatterns:    docker.ParseExcludePatterns(body.ExcludePatterns),
		Compression:        dvcrypto.NormalizeCompression(body.Compression),
		DBHotDump:          dbHotDump,
		Trigger:            "manual",
		Note:               body.Note,
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
	} else if dvcrypto.IsZstdFile(safeName) || dvcrypto.NormalizeCompression(logEntry.Compression) == "zstd" {
		c.Set("Content-Type", "application/zstd")
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

	decTar, err := dvcrypto.OpenCompressedReader(reader, logEntry.FileName)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid compressed archive: " + err.Error()})
	}
	defer decTar.Close()

	type archiveEntry struct {
		Name    string    `json:"name"`
		Size    int64     `json:"size"`
		IsDir   bool      `json:"isDir"`
		ModTime time.Time `json:"modTime"`
	}

	const maxEntries = 500
	tr := tar.NewReader(decTar)
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

	decTar, err := dvcrypto.OpenCompressedReader(reader, logEntry.FileName)
	if err != nil {
		_ = reader.Close()
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid compressed archive: " + err.Error()})
	}

	tr := tar.NewReader(decTar)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			_ = decTar.Close()
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
			return c.SendStream(multiCloseReader{r: tr, closers: []io.Closer{decTar, reader}}, int(hdr.Size))
		}
	}

	_ = decTar.Close()
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
	res, status, err := s.verifyBackupByID(c.UserContext(), c.Params("id"))
	if err != nil {
		return c.Status(status).JSON(fiber.Map{"ok": false, "match": false, "error": err.Error()})
	}
	return c.JSON(res)
}

type verifyResult struct {
	OK        bool   `json:"ok"`
	Match     bool   `json:"match"`
	Expected  string `json:"expected"`
	Actual    string `json:"actual"`
	BytesRead int64  `json:"bytesRead"`
}

func (s *Server) verifyBackupByID(ctx context.Context, rawID string) (*verifyResult, int, error) {
	logEntry, provider, driver, status, errMsg := s.openBackupContext(rawID)
	if status != 0 {
		return nil, status, errors.New(errMsg)
	}

	reader, _, err := driver.Open(ctx, logEntry.StoragePath)
	if err != nil {
		return nil, httpStatusForArchiveErr(err), err
	}
	defer reader.Close()

	hasher := sha256.New()
	bytesRead, err := io.Copy(hasher, reader)
	if err != nil {
		return nil, fiber.StatusInternalServerError, err
	}

	actual := hex.EncodeToString(hasher.Sum(nil))
	match := logEntry.ChecksumSHA256 == "" || actual == logEntry.ChecksumSHA256

	// For encrypted backups, also verify passphrase decryption succeeds when a checksum matched.
	if match && (logEntry.Encrypted || strings.HasSuffix(logEntry.StoragePath, ".enc")) {
		decReader, _, decErr := s.openArchiveStream(ctx, logEntry, provider, driver, true)
		if decErr != nil {
			return nil, fiber.StatusBadRequest, decErr
		}
		_, copyErr := io.Copy(io.Discard, decReader)
		_ = decReader.Close()
		if copyErr != nil {
			return nil, fiber.StatusBadRequest, copyErr
		}
	}

	return &verifyResult{
		OK:        match,
		Match:     match,
		Expected:  logEntry.ChecksumSHA256,
		Actual:    actual,
		BytesRead: bytesRead,
	}, 0, nil
}

// RestoreBackup extracts a backup archive back into original mounts or a staging volume.
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
		PauseContainer   *bool    `json:"pauseContainer"`
		TargetVolumeName string   `json:"targetVolumeName"`
		PreRestoreBackup *bool    `json:"preRestoreBackup"`
		Paths            []string `json:"paths"`
	}
	if len(c.Body()) > 0 {
		_ = c.BodyParser(&body)
	}
	pause := true
	if body.PauseContainer != nil {
		pause = *body.PauseContainer
	}
	preSnap := false
	if body.PreRestoreBackup != nil {
		preSnap = *body.PreRestoreBackup
	}

	res, err := s.dockerSvc.RestoreBackupWithOptions(c.UserContext(), logEntry, docker.RestoreOptions{
		PauseContainer:   pause,
		TargetVolumeName: strings.TrimSpace(body.TargetVolumeName),
		PreRestoreBackup: preSnap,
		Paths:            body.Paths,
	})
	if err != nil {
		if errors.Is(err, docker.ErrBackupInProgress) || errors.Is(err, storage.ErrAzureArchiveTier) {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(res)
}

// GetBackupProgress returns live progress (if active/recent) or persisted step timeline for a backup job.
func (s *Server) GetBackupProgress(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid backup id"})
	}
	if job, ok := s.dockerSvc.Progress().Get(uint(id)); ok {
		return c.JSON(fiber.Map{"job": job})
	}
	var logEntry models.BackupLog
	if err := s.db.First(&logEntry, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "backup log not found"})
	}
	var steps []docker.JobStep
	if logEntry.StepsJSON != "" {
		_ = json.Unmarshal([]byte(logEntry.StepsJSON), &steps)
	}
	return c.JSON(fiber.Map{
		"job": docker.JobProgress{
			BackupID:       logEntry.ID,
			TargetName:     logEntry.TargetName,
			TargetType:     logEntry.TargetType,
			Status:         logEntry.Status,
			Error:          logEntry.ErrorMessage,
			BytesProcessed: logEntry.SizeBytes,
			StartedAt:      logEntry.StartedAt,
			Steps:          steps,
		},
	})
}

// StreamBackupEvents streams live backup progress snapshots via Server-Sent Events (SSE).
func (s *Server) StreamBackupEvents(c *fiber.Ctx) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	hub := s.dockerSvc.Progress()
	ch, unsub := hub.Subscribe()
	initial := hub.Snapshot()

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer unsub()

		writeJobs := func(jobs []docker.JobProgress) bool {
			raw, err := json.Marshal(fiber.Map{"jobs": jobs})
			if err != nil {
				return true
			}
			if _, err := fmt.Fprintf(w, "event: progress\ndata: %s\n\n", raw); err != nil {
				return false
			}
			return w.Flush() == nil
		}

		if !writeJobs(initial) {
			return
		}

		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case jobs, ok := <-ch:
				if !ok {
					return
				}
				if !writeJobs(jobs) {
					return
				}
			case <-ticker.C:
				if _, err := w.WriteString(": keepalive\n\n"); err != nil {
					return
				}
				if err := w.Flush(); err != nil {
					return
				}
			}
		}
	})
	return nil
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
	deleteFile := c.Query("deleteFile", "true") != "false"
	if status, err := s.deleteBackupByID(c.UserContext(), uint(id), deleteFile); err != nil {
		return c.Status(status).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"deleted": true})
}

func (s *Server) deleteBackupByID(ctx context.Context, id uint, deleteFile bool) (int, error) {
	var logEntry models.BackupLog
	if err := s.db.First(&logEntry, id).Error; err != nil {
		return fiber.StatusNotFound, errors.New("backup log not found")
	}
	if logEntry.Status == "running" {
		return fiber.StatusConflict, errors.New("cannot delete a running backup")
	}

	if deleteFile && logEntry.Status == "success" && logEntry.StoragePath != "" {
		var provider models.StorageProvider
		if err := s.db.First(&provider, logEntry.StorageProviderID).Error; err == nil {
			if driver, err := storage.NewDriver(provider, s.cfg.BackupDir); err == nil {
				_ = driver.Delete(ctx, logEntry.StoragePath)
			}
		}
	}

	if err := s.db.Delete(&logEntry).Error; err != nil {
		return fiber.StatusInternalServerError, err
	}
	return 0, nil
}

type restoreStateSummary struct {
	Restores        []models.RestoreLog
	ActiveByTarget  map[string]models.RestoreLog
	LatestByTarget  map[string]models.RestoreLog
	ActiveRollbacks []models.RestoreLog
}

// computeRestoreState loads recent restore events and determines which targets currently
// remain rolled back to a restored backup version (no newer non-pre-restore backup after restore).
func computeRestoreState(db *gorm.DB, limit int) restoreStateSummary {
	out := restoreStateSummary{
		ActiveByTarget: make(map[string]models.RestoreLog),
		LatestByTarget: make(map[string]models.RestoreLog),
	}
	if db == nil {
		return out
	}

	db.Order("restored_at desc, id desc").Limit(limit).Find(&out.Restores)
	if len(out.Restores) == 0 {
		return out
	}

	type targetBackupTime struct {
		TargetType string
		TargetName string
		StartedAt  time.Time
	}
	var recentBackups []targetBackupTime
	db.Model(&models.BackupLog{}).
		Select("target_type, target_name, started_at").
		Where("status = ? AND trigger <> ?", "success", "pre-restore").
		Order("started_at desc, id desc").
		Limit(500).
		Scan(&recentBackups)

	latestBackupAt := make(map[string]time.Time, len(recentBackups))
	for _, b := range recentBackups {
		key := b.TargetType + ":" + b.TargetName
		if _, seen := latestBackupAt[key]; !seen {
			latestBackupAt[key] = b.StartedAt
		}
	}

	checkedInPlace := make(map[string]bool)
	for _, r := range out.Restores {
		key := r.TargetType + ":" + r.TargetName
		if _, seen := out.LatestByTarget[key]; !seen {
			out.LatestByTarget[key] = r
		}
		if r.Status == "success" && r.Mode == "inplace" && !checkedInPlace[key] {
			checkedInPlace[key] = true
			newerBackupAt, hasBackup := latestBackupAt[key]
			if !hasBackup || !newerBackupAt.After(r.RestoredAt) {
				out.ActiveByTarget[key] = r
				out.ActiveRollbacks = append(out.ActiveRollbacks, r)
			}
		}
	}
	return out
}

func resolveTargetRestore(
	targetType, targetName, composeProject string,
	state restoreStateSummary,
) *docker.RestoreSummary {
	key := targetType + ":" + targetName
	var chosen *models.RestoreLog
	isActive := false

	if act, ok := state.ActiveByTarget[key]; ok {
		c := act
		chosen = &c
		isActive = true
	} else if targetType == "container" && composeProject != "" {
		if stackAct, ok := state.ActiveByTarget["stack:"+composeProject]; ok {
			c := stackAct
			chosen = &c
			isActive = true
		}
	}

	if chosen == nil {
		if latest, ok := state.LatestByTarget[key]; ok {
			c := latest
			chosen = &c
		} else if targetType == "container" && composeProject != "" {
			if stackLatest, ok := state.LatestByTarget["stack:"+composeProject]; ok {
				c := stackLatest
				chosen = &c
			}
		}
	}

	if chosen == nil {
		return nil
	}
	return &docker.RestoreSummary{
		RestoreID:        chosen.ID,
		BackupLogID:      chosen.BackupLogID,
		BackupFileName:   chosen.BackupFileName,
		BackupStartedAt:  chosen.BackupStartedAt,
		Mode:             chosen.Mode,
		TargetVolume:     chosen.TargetVolume,
		RestoredPaths:    chosen.RestoredPaths,
		Status:           chosen.Status,
		RestoredAt:       chosen.RestoredAt,
		IsActiveRollback: isActive,
	}
}
