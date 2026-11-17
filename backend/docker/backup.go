package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/pkg/stdcopy"
	dvcrypto "github.com/muecahit94/dockvault/crypto"
	"github.com/muecahit94/dockvault/models"
	"github.com/muecahit94/dockvault/notify"
	"github.com/muecahit94/dockvault/storage"
)

// ErrBackupInProgress is returned when a backup for the same target is already running.
var ErrBackupInProgress = errors.New("a backup for this target is already in progress")

// BackupRequest specifies the parameters for running a backup job.
type BackupRequest struct {
	TargetType         string
	TargetID           string
	StorageProviderID  uint
	SecondaryStorageID *uint
	PauseContainer     bool
	SelectedMounts     []string
	PreCommand         string
	PostCommand        string
	ExcludePatterns    []string
	Compression        string // "gzip" or "zstd"
	DBHotDump          bool
	Trigger            string // "manual", "scheduled", or "pre-restore"
	ScheduleID         *uint
	RetentionPolicy    string // "count" or "gfs"
	RetentionCount     int
	KeepDaily          int
	KeepWeekly         int
	KeepMonthly        int
}

// RestoreOptions configures staging-volume and pre-restore safety snapshot behavior.
type RestoreOptions struct {
	PauseContainer   bool
	TargetVolumeName string
	PreRestoreBackup bool
}

// RestoreResult summarizes a completed volume restore operation.
type RestoreResult struct {
	Restored           bool     `json:"restored"`
	TargetName         string   `json:"targetName"`
	RestoredMounts     int      `json:"restoredMounts"`
	RestoredVolume     string   `json:"restoredVolume,omitempty"`
	PreRestoreBackupID *uint    `json:"preRestoreBackupId,omitempty"`
	PausedContainers   []string `json:"pausedContainers"`
	DurationMs         int64    `json:"durationMs"`
}

type backupMountSpec struct {
	MountInfo
	ArchiveDir string `json:"archiveDir"`
}

var nonSlugChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

type preparedBackup struct {
	req               BackupRequest
	lockKey           string
	provider          models.StorageProvider
	driver            storage.Driver
	secondaryProvider *models.StorageProvider
	secondaryDriver   storage.Driver
	targets           []ContainerInfo
	targetName        string
	mounts            []backupMountSpec
	logEntry          models.BackupLog
	startedAt         time.Time
}

// ExecuteBackup performs the safe backup workflow synchronously.
func (s *Service) ExecuteBackup(ctx context.Context, req BackupRequest) (*models.BackupLog, error) {
	prep, err := s.prepareBackup(ctx, req)
	if err != nil {
		return nil, err
	}
	return s.executePrepared(ctx, prep)
}

// StartBackupAsync validates and starts a backup asynchronously, returning the initial "running" BackupLog.
func (s *Service) StartBackupAsync(ctx context.Context, req BackupRequest) (*models.BackupLog, error) {
	prep, err := s.prepareBackup(ctx, req)
	if err != nil {
		return nil, err
	}
	go func() {
		runCtx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		if _, err := s.executePrepared(runCtx, prep); err != nil {
			log.Printf("dockvault: async backup %d (%s) failed: %v", prep.logEntry.ID, prep.targetName, err)
		}
	}()
	logCopy := prep.logEntry
	return &logCopy, nil
}

// ResolvePassphrase returns the encryption passphrase for a provider (falling back to global key).
func (s *Service) ResolvePassphrase(provider models.StorageProvider) string {
	if strings.TrimSpace(provider.EncryptionPassphrase) != "" {
		return provider.EncryptionPassphrase
	}
	return s.defaultEncryptionKey
}

func (s *Service) prepareBackup(ctx context.Context, req BackupRequest) (*preparedBackup, error) {
	if req.TargetType != "container" && req.TargetType != "stack" && req.TargetType != "volume" {
		return nil, fmt.Errorf("invalid target type %q: must be 'container', 'stack', or 'volume'", req.TargetType)
	}
	if strings.TrimSpace(req.TargetID) == "" {
		return nil, fmt.Errorf("targetId is required")
	}
	if req.Trigger == "" {
		req.Trigger = "manual"
	}

	lockKey := req.TargetType + ":" + req.TargetID
	if !s.acquireTargetLock(lockKey) {
		return nil, ErrBackupInProgress
	}

	releaseOnErr := true
	defer func() {
		if releaseOnErr {
			s.releaseTargetLock(lockKey)
		}
	}()

	provider, err := s.resolveStorageProvider(req.StorageProviderID)
	if err != nil {
		return nil, err
	}
	if provider.EncryptBackups && s.ResolvePassphrase(provider) == "" {
		return nil, fmt.Errorf("encryption enabled on storage %q but no passphrase is configured", provider.Name)
	}
	driver, err := storage.NewDriver(provider, s.backupDir)
	if err != nil {
		return nil, fmt.Errorf("initialize storage driver: %w", err)
	}

	var secProviderPtr *models.StorageProvider
	var secDriver storage.Driver
	if req.SecondaryStorageID != nil && *req.SecondaryStorageID > 0 && *req.SecondaryStorageID != provider.ID {
		secProv, secErr := s.resolveStorageProvider(*req.SecondaryStorageID)
		if secErr != nil {
			return nil, fmt.Errorf("secondary storage: %w", secErr)
		}
		if secProv.EncryptBackups && s.ResolvePassphrase(secProv) == "" {
			return nil, fmt.Errorf("encryption enabled on secondary storage %q but no passphrase is configured", secProv.Name)
		}
		secDrv, drvErr := storage.NewDriver(secProv, s.backupDir)
		if drvErr != nil {
			return nil, fmt.Errorf("initialize secondary storage driver: %w", drvErr)
		}
		secProviderPtr = &secProv
		secDriver = secDrv
	}

	targets, targetName, mounts, err := s.resolveTargets(ctx, req.TargetType, req.TargetID, req.SelectedMounts)
	if err != nil {
		return nil, err
	}
	if len(mounts) == 0 {
		return nil, fmt.Errorf("no backupable volumes or bind mounts found for %s %q", req.TargetType, targetName)
	}

	startedAt := time.Now().UTC()
	volumesJSON, _ := json.Marshal(mounts)
	compression := dvcrypto.NormalizeCompression(req.Compression)
	excludes := normalizeExcludeList(req.ExcludePatterns)
	if len(excludes) == 0 && len(targets) > 0 {
		var labelParts []string
		for _, c := range targets {
			if c.GitOpsExcludePaths != "" {
				labelParts = append(labelParts, c.GitOpsExcludePaths)
			}
		}
		if len(labelParts) > 0 {
			excludes = ParseExcludePatterns(strings.Join(labelParts, "\n"))
		}
	}
	req.Compression = compression
	req.ExcludePatterns = excludes

	logEntry := models.BackupLog{
		ScheduleID:          req.ScheduleID,
		Trigger:             req.Trigger,
		TargetType:          req.TargetType,
		TargetID:            req.TargetID,
		TargetName:          targetName,
		StorageProviderID:   provider.ID,
		StorageProviderName: provider.Name,
		StorageType:         provider.Type,
		AccessTier:          azureTierOrEmpty(provider),
		Compression:         compression,
		ExcludePatterns:     strings.Join(excludes, "\n"),
		Encrypted:           provider.EncryptBackups,
		Status:              "running",
		VolumeCount:         len(mounts),
		VolumesJSON:         string(volumesJSON),
		StartedAt:           startedAt,
	}
	if err := s.db.Create(&logEntry).Error; err != nil {
		return nil, fmt.Errorf("create backup log: %w", err)
	}

	releaseOnErr = false
	return &preparedBackup{
		req:               req,
		lockKey:           lockKey,
		provider:          provider,
		driver:            driver,
		secondaryProvider: secProviderPtr,
		secondaryDriver:   secDriver,
		targets:           targets,
		targetName:        targetName,
		mounts:            mounts,
		logEntry:          logEntry,
		startedAt:         startedAt,
	}, nil
}

func (s *Service) executePrepared(ctx context.Context, prep *preparedBackup) (*models.BackupLog, error) {
	defer s.releaseTargetLock(prep.lockKey)

	logEntry := prep.logEntry
	startedAt := prep.startedAt
	tracker := s.progress.StartJob(logEntry.ID, prep.targetName, prep.req.TargetType)

	failBackup := func(runErr error) (*models.BackupLog, error) {
		completed := time.Now().UTC()
		logEntry.Status = "failed"
		logEntry.ErrorMessage = runErr.Error()
		logEntry.CompletedAt = &completed
		logEntry.DurationMs = completed.Sub(startedAt).Milliseconds()
		logEntry.StepsJSON = tracker.Finish("failed", runErr.Error())
		s.db.Save(&logEntry)
		go notify.DispatchBackupAlert(context.Background(), s.db, &logEntry)
		return &logEntry, runErr
	}

	tracker.BeginStep("helper", "Prepare Archive Helper", s.helperImage)
	if err := s.ensureHelperImage(ctx); err != nil {
		return failBackup(fmt.Errorf("prepare helper image %s: %w", s.helperImage, err))
	}

	// 0. Optional zero-downtime DB hot dumps (containers with a successful dump stay unpaused).
	var dbDumps []dbDumpSpec
	defer func() {
		for _, d := range dbDumps {
			_ = os.Remove(d.TempPath)
		}
	}()
	dumpedContainers := make(map[string]bool)
	if prep.req.DBHotDump {
		tracker.BeginStep("db_dump", "Database Hot Dumps", "Capturing consistent database snapshots without downtime")
		var dumpedNames []string
		for _, c := range prep.targets {
			if c.IsSelf || c.State != "running" || c.DetectedDB == "" {
				continue
			}
			tracker.AddLog(fmt.Sprintf("Running %s hot dump on %s...", c.DetectedDB, c.Name))
			spec, dumpErr := s.captureDBHotDump(ctx, c)
			if dumpErr != nil {
				return failBackup(dumpErr)
			}
			if spec != nil {
				dbDumps = append(dbDumps, *spec)
				dumpedContainers[c.ID] = true
				dumpedNames = append(dumpedNames, fmt.Sprintf("%s (%s)", c.Name, c.DetectedDB))
			}
		}
		if len(dumpedNames) == 0 {
			tracker.CompleteCurrentStep("No running databases matched")
		} else {
			tracker.CompleteCurrentStep("Captured " + strings.Join(dumpedNames, ", "))
		}
	}

	// 1. Run pre-backup exec hooks on running containers (label or schedule-level).
	postHooksRan := false
	runPostHooks := func() {
		if postHooksRan {
			return
		}
		postHooksRan = true
		hookCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		for _, c := range prep.targets {
			if c.IsSelf || c.State != "running" {
				continue
			}
			cmd := firstNonEmptyStr(prep.req.PostCommand, c.PostHook)
			if cmd != "" {
				if err := s.execInContainer(hookCtx, c.ID, cmd); err != nil {
					log.Printf("dockvault: warning: post-backup hook on %s failed: %v", c.Name, err)
					tracker.AddLog(fmt.Sprintf("Post-hook warning on %s: %v", c.Name, err))
				}
			}
		}
	}
	defer runPostHooks()

	hasPreHooks := false
	for _, c := range prep.targets {
		if !c.IsSelf && c.State == "running" && firstNonEmptyStr(prep.req.PreCommand, c.PreHook) != "" {
			hasPreHooks = true
			break
		}
	}
	if hasPreHooks {
		tracker.BeginStep("pre_hooks", "Pre-Backup Hooks", "Running container exec hooks")
		for _, c := range prep.targets {
			if c.IsSelf || c.State != "running" {
				continue
			}
			cmd := firstNonEmptyStr(prep.req.PreCommand, c.PreHook)
			if cmd != "" {
				if err := s.execInContainer(ctx, c.ID, cmd); err != nil {
					return failBackup(fmt.Errorf("pre-backup hook on %s failed: %w", c.Name, err))
				}
			}
		}
	} else {
		tracker.SkipStep("Pre-Backup Hooks", "None configured")
	}

	// 2. Pause running non-self containers (skipping zero-downtime DB containers) and guarantee unpause.
	var pausedNames []string
	var pausedIDs []string
	unpauseAll := func() {
		if len(pausedIDs) == 0 {
			return
		}
		unpauseCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, id := range pausedIDs {
			if err := s.cli.ContainerUnpause(unpauseCtx, id); err != nil {
				log.Printf("dockvault: warning: failed to unpause container %s: %v", id, err)
			}
		}
		pausedIDs = nil
	}
	defer unpauseAll()

	if prep.req.PauseContainer {
		var toPause []ContainerInfo
		for _, c := range prep.targets {
			if c.IsSelf || c.State != "running" || dumpedContainers[c.ID] {
				continue
			}
			toPause = append(toPause, c)
		}
		if len(toPause) > 0 {
			tracker.BeginStep("pause", "Pause Containers", fmt.Sprintf("Pausing %d container(s)", len(toPause)))
			for _, c := range toPause {
				if err := s.cli.ContainerPause(ctx, c.ID); err != nil {
					return failBackup(fmt.Errorf("pause container %s: %w", c.Name, err))
				}
				pausedIDs = append(pausedIDs, c.ID)
				pausedNames = append(pausedNames, c.Name)
			}
		} else {
			tracker.SkipStep("Pause Containers", "Zero-downtime / no running containers to pause")
		}
	} else {
		tracker.SkipStep("Pause Containers", "Disabled")
	}

	pausedJSON, _ := json.Marshal(pausedNames)
	logEntry.PausedContainersJSON = string(pausedJSON)

	ext := dvcrypto.ArchiveExtension(prep.req.Compression)
	baseSlug := fmt.Sprintf("%s-%s", sanitizeSlug(prep.targetName), startedAt.Format("20060102-150405"))
	tempPlain, err := os.CreateTemp(s.backupDir, "dockvault-plain-*"+ext)
	if err != nil {
		return failBackup(fmt.Errorf("create temp archive: %w", err))
	}
	plainPath := tempPlain.Name()
	_ = tempPlain.Close()
	defer os.Remove(plainPath)

	archDetail := fmt.Sprintf("%s compression · %d mount(s)", strings.ToUpper(prep.req.Compression), len(prep.mounts))
	if len(prep.req.ExcludePatterns) > 0 {
		archDetail += fmt.Sprintf(" · %d exclude(s)", len(prep.req.ExcludePatterns))
	}
	tracker.BeginStep("archive", "Archive & Compress", archDetail)

	plainSize, plainChecksum, err := s.runArchiveHelper(
		ctx, prep.mounts, prep.req.ExcludePatterns, prep.req.Compression, dbDumps, plainPath, tracker,
	)
	// Unpause and run post-hooks immediately after archiving so container downtime stays minimal.
	unpauseAll()
	runPostHooks()

	if err != nil {
		return failBackup(err)
	}

	// 3. Store to primary destination (encrypting if configured on primary provider).
	uploadDetail := prep.provider.Name
	if prep.provider.EncryptBackups {
		uploadDetail += " (AES-256-GCM)"
	}
	tracker.BeginStep("upload", "Encrypt & Store Primary", uploadDetail)
	primaryFileName, primaryPath, primarySize, primaryChecksum, err := s.saveArchiveForProvider(
		ctx, prep.provider, prep.driver, plainPath, plainSize, plainChecksum, baseSlug, ext,
	)
	if err != nil {
		return failBackup(err)
	}

	// 4. Optional 3-2-1 secondary offsite copy (runs independently using the captured plaintext archive).
	if prep.secondaryProvider != nil && prep.secondaryDriver != nil {
		tracker.BeginStep("secondary", "3-2-1 Offsite Replication", prep.secondaryProvider.Name)
		s.replicateToSecondary(ctx, prep, &logEntry, plainPath, plainSize, plainChecksum, baseSlug, ext)
	}

	// 5. Retention pruning (pin-protected, count or GFS).
	if prep.req.ScheduleID != nil {
		tracker.BeginStep("retention", "Retention Pruning", prep.req.RetentionPolicy)
		s.pruneScheduleRetention(ctx, *prep.req.ScheduleID, RetentionPolicyConfig{
			Policy:      prep.req.RetentionPolicy,
			Count:       prep.req.RetentionCount,
			KeepDaily:   prep.req.KeepDaily,
			KeepWeekly:  prep.req.KeepWeekly,
			KeepMonthly: prep.req.KeepMonthly,
		})
	}

	completed := time.Now().UTC()
	logEntry.Status = "success"
	logEntry.FileName = primaryFileName
	logEntry.StoragePath = primaryPath
	logEntry.SizeBytes = primarySize
	logEntry.ChecksumSHA256 = primaryChecksum
	logEntry.CompletedAt = &completed
	logEntry.DurationMs = completed.Sub(startedAt).Milliseconds()
	logEntry.StepsJSON = tracker.Finish("success", "")
	if err := s.db.Save(&logEntry).Error; err != nil {
		return &logEntry, fmt.Errorf("update backup log: %w", err)
	}

	go notify.DispatchBackupAlert(context.Background(), s.db, &logEntry)
	return &logEntry, nil
}

func (s *Service) saveArchiveForProvider(
	ctx context.Context,
	provider models.StorageProvider,
	driver storage.Driver,
	plainPath string,
	plainSize int64,
	plainChecksum string,
	baseSlug string,
	ext string,
) (fileName, storagePath string, sizeBytes int64, checksum string, err error) {
	uploadSource := plainPath
	fileName = baseSlug + ext
	sizeBytes = plainSize
	checksum = plainChecksum

	if provider.EncryptBackups {
		encTemp, tmpErr := os.CreateTemp(s.backupDir, "dockvault-enc-*"+ext+".enc")
		if tmpErr != nil {
			return "", "", 0, "", fmt.Errorf("create encrypted temp archive: %w", tmpErr)
		}
		encPath := encTemp.Name()
		_ = encTemp.Close()
		defer os.Remove(encPath)

		encSize, encSum, encErr := dvcrypto.EncryptFile(plainPath, encPath, s.ResolvePassphrase(provider))
		if encErr != nil {
			return "", "", 0, "", fmt.Errorf("encrypt archive: %w", encErr)
		}
		uploadSource = encPath
		fileName = baseSlug + ext + ".enc"
		sizeBytes = encSize
		checksum = encSum
	} else {
		// Copy plainPath so Save() can safely rename/remove its source without breaking secondary replication.
		copyTemp, copyErr := os.CreateTemp(s.backupDir, "dockvault-upload-*"+ext)
		if copyErr != nil {
			return "", "", 0, "", fmt.Errorf("prepare upload temp archive: %w", copyErr)
		}
		copyPath := copyTemp.Name()
		if err := copyFileContents(plainPath, copyTemp); err != nil {
			_ = copyTemp.Close()
			_ = os.Remove(copyPath)
			return "", "", 0, "", fmt.Errorf("stage archive for upload: %w", err)
		}
		_ = copyTemp.Close()
		defer os.Remove(copyPath)
		uploadSource = copyPath
	}

	storagePath, saveErr := driver.Save(ctx, uploadSource, fileName)
	if saveErr != nil {
		return "", "", 0, "", fmt.Errorf("store archive: %w", saveErr)
	}
	return fileName, storagePath, sizeBytes, checksum, nil
}

func (s *Service) replicateToSecondary(
	ctx context.Context,
	prep *preparedBackup,
	primaryLog *models.BackupLog,
	plainPath string,
	plainSize int64,
	plainChecksum string,
	baseSlug string,
	ext string,
) {
	secProv := *prep.secondaryProvider
	secLog := models.BackupLog{
		ScheduleID:           primaryLog.ScheduleID,
		Trigger:              primaryLog.Trigger,
		TargetType:           primaryLog.TargetType,
		TargetID:             primaryLog.TargetID,
		TargetName:           primaryLog.TargetName,
		StorageProviderID:    secProv.ID,
		StorageProviderName:  secProv.Name,
		StorageType:          secProv.Type,
		AccessTier:           azureTierOrEmpty(secProv),
		Compression:          primaryLog.Compression,
		ExcludePatterns:      primaryLog.ExcludePatterns,
		Encrypted:            secProv.EncryptBackups,
		Status:               "running",
		VolumeCount:          primaryLog.VolumeCount,
		VolumesJSON:          primaryLog.VolumesJSON,
		PausedContainersJSON: primaryLog.PausedContainersJSON,
		StartedAt:            time.Now().UTC(),
	}
	_ = s.db.Create(&secLog).Error

	fName, sPath, sz, sum, err := s.saveArchiveForProvider(
		ctx, secProv, prep.secondaryDriver, plainPath, plainSize, plainChecksum, baseSlug, ext,
	)
	done := time.Now().UTC()
	secLog.CompletedAt = &done
	secLog.DurationMs = done.Sub(secLog.StartedAt).Milliseconds()
	if err != nil {
		secLog.Status = "failed"
		secLog.ErrorMessage = err.Error()
		_ = s.db.Save(&secLog).Error
		go notify.DispatchBackupAlert(context.Background(), s.db, &secLog)
		return
	}
	secLog.Status = "success"
	secLog.FileName = fName
	secLog.StoragePath = sPath
	secLog.SizeBytes = sz
	secLog.ChecksumSHA256 = sum
	_ = s.db.Save(&secLog).Error
	go notify.DispatchBackupAlert(context.Background(), s.db, &secLog)
}

// RestoreBackup extracts a stored archive back into its original Docker volumes/bind mounts.
func (s *Service) RestoreBackup(ctx context.Context, logEntry models.BackupLog, pauseContainer bool) (*RestoreResult, error) {
	return s.RestoreBackupWithOptions(ctx, logEntry, RestoreOptions{PauseContainer: pauseContainer})
}

// RestoreBackupWithOptions extracts a backup into original mounts or a staging volume,
// optionally taking a pre-restore safety snapshot before overwriting live data.
func (s *Service) RestoreBackupWithOptions(ctx context.Context, logEntry models.BackupLog, opts RestoreOptions) (*RestoreResult, error) {
	if logEntry.Status != "success" || logEntry.StoragePath == "" {
		return nil, fmt.Errorf("backup archive is not available for restore")
	}

	var mounts []backupMountSpec
	if err := json.Unmarshal([]byte(logEntry.VolumesJSON), &mounts); err != nil || len(mounts) == 0 {
		return nil, fmt.Errorf("backup archive contains no restorable mount metadata")
	}

	stagingVol := strings.TrimSpace(opts.TargetVolumeName)
	if stagingVol != "" {
		if _, err := storage.SafeFileName(stagingVol); err != nil {
			return nil, fmt.Errorf("invalid staging volume name %q", stagingVol)
		}
		if _, err := s.cli.VolumeCreate(ctx, volume.CreateOptions{Name: stagingVol}); err != nil {
			return nil, fmt.Errorf("create staging volume %q: %w", stagingVol, err)
		}
	}

	var preRestoreID *uint
	if stagingVol == "" && opts.PreRestoreBackup {
		snap, snapErr := s.ExecuteBackup(ctx, BackupRequest{
			TargetType:        logEntry.TargetType,
			TargetID:          logEntry.TargetID,
			StorageProviderID: logEntry.StorageProviderID,
			PauseContainer:    opts.PauseContainer,
			Compression:       logEntry.Compression,
			Trigger:           "pre-restore",
		})
		if snapErr != nil {
			return nil, fmt.Errorf("pre-restore safety snapshot failed (restore aborted): %w", snapErr)
		}
		if snap != nil {
			preRestoreID = &snap.ID
		}
	}

	lockKey := logEntry.TargetType + ":" + logEntry.TargetID
	if !s.acquireTargetLock(lockKey) {
		return nil, ErrBackupInProgress
	}
	defer s.releaseTargetLock(lockKey)

	provider, err := s.resolveStorageProvider(logEntry.StorageProviderID)
	if err != nil {
		return nil, err
	}
	driver, err := storage.NewDriver(provider, s.backupDir)
	if err != nil {
		return nil, fmt.Errorf("initialize storage driver: %w", err)
	}

	rawStream, _, err := driver.Open(ctx, logEntry.StoragePath)
	if err != nil {
		return nil, err
	}
	archiveStream := rawStream
	if logEntry.Encrypted || strings.HasSuffix(logEntry.StoragePath, ".enc") {
		decStream, decErr := dvcrypto.DecryptReader(rawStream, s.ResolvePassphrase(provider))
		if decErr != nil {
			return nil, decErr
		}
		archiveStream = decStream
	}
	defer archiveStream.Close()

	tarStream, err := dvcrypto.OpenCompressedReader(archiveStream, logEntry.FileName)
	if err != nil {
		return nil, fmt.Errorf("decompress archive: %w", err)
	}
	defer tarStream.Close()

	if err := s.ensureHelperImage(ctx); err != nil {
		return nil, fmt.Errorf("prepare helper image %s: %w", s.helperImage, err)
	}

	startedAt := time.Now().UTC()
	var pausedIDs []string
	var pausedNames []string
	unpauseAll := func() {
		if len(pausedIDs) == 0 {
			return
		}
		unpauseCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, id := range pausedIDs {
			if err := s.cli.ContainerUnpause(unpauseCtx, id); err != nil {
				log.Printf("dockvault: warning: failed to unpause container %s after restore: %v", id, err)
			}
		}
		pausedIDs = nil
	}
	defer unpauseAll()

	if stagingVol == "" && opts.PauseContainer && (logEntry.TargetType == "container" || logEntry.TargetType == "stack") {
		if targets, _, _, err := s.resolveTargets(ctx, logEntry.TargetType, logEntry.TargetID, nil); err == nil {
			for _, c := range targets {
				if c.IsSelf || c.State != "running" {
					continue
				}
				if err := s.cli.ContainerPause(ctx, c.ID); err != nil {
					return nil, fmt.Errorf("pause container %s before restore: %w", c.Name, err)
				}
				pausedIDs = append(pausedIDs, c.ID)
				pausedNames = append(pausedNames, c.Name)
			}
		}
	}

	if err := s.runRestoreHelper(ctx, mounts, stagingVol, tarStream); err != nil {
		return nil, err
	}
	unpauseAll()

	return &RestoreResult{
		Restored:           true,
		TargetName:         logEntry.TargetName,
		RestoredMounts:     len(mounts),
		RestoredVolume:     stagingVol,
		PreRestoreBackupID: preRestoreID,
		PausedContainers:   pausedNames,
		DurationMs:         time.Since(startedAt).Milliseconds(),
	}, nil
}

func (s *Service) execInContainer(ctx context.Context, containerID, shellCmd string) error {
	execResp, err := s.cli.ContainerExecCreate(ctx, containerID, container.ExecOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          []string{"sh", "-c", shellCmd},
	})
	if err != nil {
		return fmt.Errorf("create exec: %w", err)
	}
	attachResp, err := s.cli.ContainerExecAttach(ctx, execResp.ID, container.ExecStartOptions{})
	if err != nil {
		return fmt.Errorf("attach exec: %w", err)
	}
	defer attachResp.Close()

	var stderrBuf bytes.Buffer
	_, _ = stdcopy.StdCopy(io.Discard, &stderrBuf, attachResp.Reader)

	inspect, err := s.cli.ContainerExecInspect(ctx, execResp.ID)
	if err != nil {
		return fmt.Errorf("inspect exec: %w", err)
	}
	if inspect.ExitCode != 0 {
		msg := strings.TrimSpace(stderrBuf.String())
		if msg == "" {
			msg = fmt.Sprintf("exit code %d", inspect.ExitCode)
		}
		return fmt.Errorf("hook %q failed: %s", shellCmd, msg)
	}
	return nil
}

func (s *Service) resolveStorageProvider(providerID uint) (models.StorageProvider, error) {
	var provider models.StorageProvider
	if providerID > 0 {
		if err := s.db.First(&provider, providerID).Error; err != nil {
			return provider, fmt.Errorf("storage provider %d not found: %w", providerID, err)
		}
		return provider, nil
	}
	if err := s.db.Where("is_default = ?", true).First(&provider).Error; err == nil {
		return provider, nil
	}
	if err := s.db.Order("id asc").First(&provider).Error; err != nil {
		return provider, fmt.Errorf("no storage provider configured: %w", err)
	}
	return provider, nil
}

func (s *Service) resolveTargets(ctx context.Context, targetType, targetID string, selectedMounts []string) ([]ContainerInfo, string, []backupMountSpec, error) {
	if targetType == "volume" {
		vol, err := s.cli.VolumeInspect(ctx, targetID)
		if err != nil {
			return nil, "", nil, fmt.Errorf("volume %q not found: %w", targetID, err)
		}
		mi := MountInfo{
			Type:        "volume",
			Name:        vol.Name,
			Source:      vol.Mountpoint,
			Destination: "/volume",
			RW:          true,
			Backupable:  true,
		}
		return nil, vol.Name, []backupMountSpec{{
			MountInfo:  mi,
			ArchiveDir: "vol_" + sanitizeSlug(vol.Name),
		}}, nil
	}

	allContainers, err := s.ListContainers(ctx, true)
	if err != nil {
		return nil, "", nil, err
	}

	var matched []ContainerInfo
	targetName := targetID

	switch targetType {
	case "container":
		for _, c := range allContainers {
			if c.ID == targetID || c.ShortID == targetID || c.Name == targetID {
				matched = append(matched, c)
				targetName = c.Name
				break
			}
		}
		if len(matched) == 0 {
			return nil, "", nil, fmt.Errorf("container %q not found", targetID)
		}
	case "stack":
		for _, c := range allContainers {
			if c.ComposeProject == targetID && !c.Excluded {
				matched = append(matched, c)
			}
		}
		if len(matched) == 0 {
			return nil, "", nil, fmt.Errorf("compose stack %q not found", targetID)
		}
	}

	selectedSet := make(map[string]bool, len(selectedMounts))
	for _, sm := range selectedMounts {
		if trimmed := strings.TrimSpace(sm); trimmed != "" {
			selectedSet[trimmed] = true
		}
	}

	seen := make(map[string]bool)
	usedDirNames := make(map[string]int)
	var mounts []backupMountSpec

	for _, c := range matched {
		for _, m := range c.Mounts {
			if !m.Backupable {
				continue
			}
			if len(selectedSet) > 0 && !selectedSet[m.Source] && !selectedSet[m.Name] && !selectedSet[m.Destination] {
				continue
			}
			key := mountDedupKey(m)
			if seen[key] {
				continue
			}
			seen[key] = true

			baseDirName := sanitizeSlug(m.Name)
			if m.Type == "bind" || baseDirName == "" {
				baseDirName = "bind_" + sanitizeSlug(filepath.Base(m.Destination))
			} else {
				baseDirName = "vol_" + baseDirName
			}

			usedDirNames[baseDirName]++
			dirName := baseDirName
			if usedDirNames[baseDirName] > 1 {
				dirName = fmt.Sprintf("%s_%d", baseDirName, usedDirNames[baseDirName])
			}

			mounts = append(mounts, backupMountSpec{
				MountInfo:  m,
				ArchiveDir: dirName,
			})
		}
	}

	return matched, targetName, mounts, nil
}

func (s *Service) ensureHelperImage(ctx context.Context) error {
	_, err := s.cli.ImageInspect(ctx, s.helperImage)
	if err == nil {
		return nil
	}

	reader, err := s.cli.ImagePull(ctx, s.helperImage, image.PullOptions{})
	if err != nil {
		return err
	}
	defer reader.Close()
	_, _ = io.Copy(io.Discard, reader)
	return nil
}

func (s *Service) runArchiveHelper(
	ctx context.Context,
	mounts []backupMountSpec,
	excludes []string,
	compression string,
	dbDumps []dbDumpSpec,
	tempPath string,
	tracker *JobTracker,
) (int64, string, error) {
	binds := make([]string, 0, len(mounts))
	for _, m := range mounts {
		src := m.Source
		if m.Type == "volume" && m.Name != "" {
			src = m.Name
		}
		binds = append(binds, fmt.Sprintf("%s:/source/%s:ro", src, m.ArchiveDir))
	}

	tarCmd := []string{"tar", "-cf", "-"}
	for _, ex := range excludes {
		tarCmd = append(tarCmd, "--exclude="+ex)
	}
	tarCmd = append(tarCmd, "-C", "/source", ".")

	resp, err := s.cli.ContainerCreate(
		ctx,
		&container.Config{
			Image:        s.helperImage,
			Cmd:          tarCmd,
			AttachStdout: true,
			AttachStderr: true,
			Tty:          false,
			Labels: map[string]string{
				"dockvault.helper": "true",
			},
		},
		&container.HostConfig{
			Binds: binds,
		},
		nil,
		nil,
		"",
	)
	if err != nil {
		return 0, "", fmt.Errorf("create helper container: %w", err)
	}

	helperID := resp.ID
	defer func() {
		rmCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = s.cli.ContainerRemove(rmCtx, helperID, container.RemoveOptions{Force: true, RemoveVolumes: false})
	}()

	attachResp, err := s.cli.ContainerAttach(ctx, helperID, container.AttachOptions{
		Stream: true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return 0, "", fmt.Errorf("attach helper container: %w", err)
	}
	defer attachResp.Close()

	if err := s.cli.ContainerStart(ctx, helperID, container.StartOptions{}); err != nil {
		return 0, "", fmt.Errorf("start helper container: %w", err)
	}

	outFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return 0, "", fmt.Errorf("open temp archive for writing: %w", err)
	}

	hasher := sha256.New()
	counter := &progressCountingWriter{dst: outFile, tracker: tracker}
	multiOut := io.MultiWriter(counter, hasher)

	compWriter, err := dvcrypto.NewCompressedWriter(multiOut, compression)
	if err != nil {
		_ = outFile.Close()
		return 0, "", fmt.Errorf("init compressor: %w", err)
	}

	pr, pw := io.Pipe()
	var stderrBuf bytes.Buffer
	demuxCh := make(chan error, 1)
	go func() {
		_, copyErr := stdcopy.StdCopy(pw, &stderrBuf, attachResp.Reader)
		_ = pw.CloseWithError(copyErr)
		demuxCh <- copyErr
	}()

	var compressErr error
	if len(dbDumps) == 0 {
		_, compressErr = io.Copy(compWriter, pr)
	} else {
		compressErr = copyTarAndAppendDB(pr, compWriter, dbDumps)
	}
	_ = pr.Close()
	demuxErr := <-demuxCh
	compCloseErr := compWriter.Close()
	fileCloseErr := outFile.Close()

	if demuxErr != nil {
		return 0, "", fmt.Errorf("stream archive from helper: %w", demuxErr)
	}
	if compressErr != nil {
		return 0, "", fmt.Errorf("compress archive: %w", compressErr)
	}
	if compCloseErr != nil {
		return 0, "", fmt.Errorf("finalize compressed archive: %w", compCloseErr)
	}
	if fileCloseErr != nil {
		return 0, "", fmt.Errorf("close temp archive: %w", fileCloseErr)
	}

	waitCh, errCh := s.cli.ContainerWait(ctx, helperID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return 0, "", fmt.Errorf("wait for helper container: %w", err)
		}
	case status := <-waitCh:
		if status.StatusCode != 0 {
			errMsg := strings.TrimSpace(stderrBuf.String())
			if errMsg == "" {
				errMsg = fmt.Sprintf("tar exited with status %d", status.StatusCode)
			}
			return 0, "", fmt.Errorf("archive failed: %s", errMsg)
		}
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))
	return counter.total, checksum, nil
}

func copyTarAndAppendDB(src io.Reader, dst io.Writer, dumps []dbDumpSpec) error {
	tr := tar.NewReader(src)
	tw := tar.NewWriter(dst)

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if hdr.Size > 0 {
			if _, err := io.Copy(tw, tr); err != nil {
				return err
			}
		}
	}

	now := time.Now().UTC()
	if err := tw.WriteHeader(&tar.Header{
		Name:     "db_dumps/",
		Mode:     0o755,
		Typeflag: tar.TypeDir,
		ModTime:  now,
	}); err != nil {
		return err
	}

	for _, d := range dumps {
		f, err := os.Open(d.TempPath)
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{
			Name:     d.ArchivePath,
			Size:     d.Size,
			Mode:     0o640,
			Typeflag: tar.TypeReg,
			ModTime:  now,
		}); err != nil {
			_ = f.Close()
			return err
		}
		if _, err := io.Copy(tw, f); err != nil {
			_ = f.Close()
			return err
		}
		_ = f.Close()
	}

	return tw.Close()
}

func (s *Service) runRestoreHelper(ctx context.Context, mounts []backupMountSpec, stagingVol string, tarStream io.Reader) error {
	var binds []string
	if stagingVol != "" {
		binds = []string{fmt.Sprintf("%s:/restore:rw", stagingVol)}
	} else {
		binds = make([]string, 0, len(mounts))
		for _, m := range mounts {
			src := m.Source
			if m.Type == "volume" && m.Name != "" {
				src = m.Name
			}
			binds = append(binds, fmt.Sprintf("%s:/restore/%s:rw", src, m.ArchiveDir))
		}
	}

	resp, err := s.cli.ContainerCreate(
		ctx,
		&container.Config{
			Image:        s.helperImage,
			Cmd:          []string{"tar", "-xf", "-", "-C", "/restore"},
			AttachStdin:  true,
			OpenStdin:    true,
			StdinOnce:    true,
			AttachStdout: true,
			AttachStderr: true,
			Tty:          false,
			Labels: map[string]string{
				"dockvault.helper": "true",
			},
		},
		&container.HostConfig{
			Binds: binds,
		},
		nil,
		nil,
		"",
	)
	if err != nil {
		return fmt.Errorf("create restore helper container: %w", err)
	}

	helperID := resp.ID
	defer func() {
		rmCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = s.cli.ContainerRemove(rmCtx, helperID, container.RemoveOptions{Force: true, RemoveVolumes: false})
	}()

	attachResp, err := s.cli.ContainerAttach(ctx, helperID, container.AttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return fmt.Errorf("attach restore helper: %w", err)
	}
	defer attachResp.Close()

	if err := s.cli.ContainerStart(ctx, helperID, container.StartOptions{}); err != nil {
		return fmt.Errorf("start restore helper: %w", err)
	}

	var stderrBuf bytes.Buffer
	drainDone := make(chan error, 1)
	go func() {
		_, err := stdcopy.StdCopy(io.Discard, &stderrBuf, attachResp.Reader)
		drainDone <- err
	}()

	if _, err := io.Copy(attachResp.Conn, tarStream); err != nil {
		return fmt.Errorf("stream archive into restore helper: %w", err)
	}
	if err := attachResp.CloseWrite(); err != nil {
		return fmt.Errorf("close restore stdin: %w", err)
	}
	<-drainDone

	waitCh, errCh := s.cli.ContainerWait(ctx, helperID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("wait for restore helper: %w", err)
		}
	case status := <-waitCh:
		if status.StatusCode != 0 {
			errMsg := strings.TrimSpace(stderrBuf.String())
			if errMsg == "" {
				errMsg = fmt.Sprintf("tar extract exited with status %d", status.StatusCode)
			}
			return fmt.Errorf("restore failed: %s", errMsg)
		}
	}
	return nil
}

func (s *Service) pruneScheduleRetention(ctx context.Context, scheduleID uint, policy RetentionPolicyConfig) {
	var logs []models.BackupLog
	if err := s.db.Where("schedule_id = ? AND status = ?", scheduleID, "success").
		Order("started_at desc").
		Find(&logs).Error; err != nil {
		return
	}

	// Prune independently per storage provider so 3-2-1 primary & secondary copies each keep their own retention set.
	byProvider := make(map[uint][]models.BackupLog)
	for _, entry := range logs {
		byProvider[entry.StorageProviderID] = append(byProvider[entry.StorageProviderID], entry)
	}

	for _, group := range byProvider {
		for _, old := range SelectLogsToDelete(group, policy) {
			var provider models.StorageProvider
			if err := s.db.First(&provider, old.StorageProviderID).Error; err == nil {
				if driver, err := storage.NewDriver(provider, s.backupDir); err == nil {
					_ = driver.Delete(ctx, old.StoragePath)
				}
			}
			_ = s.db.Delete(&old).Error
		}
	}
}

// CleanupOrphanHelpers removes any leftover dockvault.helper containers from interrupted runs.
func (s *Service) CleanupOrphanHelpers(ctx context.Context) {
	list, err := s.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", "dockvault.helper=true")),
	})
	if err != nil {
		return
	}
	for _, c := range list {
		_ = s.cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true})
	}
}

func (s *Service) acquireTargetLock(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeTargets[key] {
		return false
	}
	s.activeTargets[key] = true
	return true
}

func (s *Service) releaseTargetLock(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.activeTargets, key)
}

func sanitizeSlug(input string) string {
	s := strings.TrimSpace(input)
	s = nonSlugChars.ReplaceAllString(s, "_")
	s = strings.Trim(s, "._-")
	if s == "" {
		return "backup"
	}
	return s
}

func azureTierOrEmpty(p models.StorageProvider) string {
	if p.Type != "azure" {
		return ""
	}
	if p.AzureAccessTier != "" {
		return p.AzureAccessTier
	}
	return "Cool"
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if t := strings.TrimSpace(v); t != "" {
			return t
		}
	}
	return ""
}

func copyFileContents(srcPath string, dst io.Writer) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	_, err = io.Copy(dst, src)
	return err
}
