package handlers

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	dvcrypto "github.com/muecahit94/dockvault/crypto"
	"github.com/muecahit94/dockvault/docker"
	"github.com/muecahit94/dockvault/models"
)

const (
	maxBackupNoteLength  = 500
	maxBulkBackupIDs     = 200
	maxCompareEntries    = 200000
	maxCompareListedDiff = 1000
)

// UpdateBackupNote sets or clears the free-text note attached to a backup.
func (s *Server) UpdateBackupNote(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid backup id"})
	}
	var body struct {
		Note string `json:"note"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON body"})
	}
	note := strings.TrimSpace(body.Note)
	if len([]rune(note)) > maxBackupNoteLength {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "note must be at most 500 characters"})
	}

	var logEntry models.BackupLog
	if err := s.db.First(&logEntry, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "backup log not found"})
	}
	if err := s.db.Model(&logEntry).Update("note", note).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	logEntry.Note = note
	return c.JSON(fiber.Map{"backup": logEntry})
}

type bulkItemResult struct {
	ID     uint   `json:"id"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	Match  *bool  `json:"match,omitempty"`
	Pinned *bool  `json:"pinned,omitempty"`
}

// BulkBackupAction applies pin, unpin, delete, or verify to many backups and reports per-item results.
func (s *Server) BulkBackupAction(c *fiber.Ctx) error {
	var body struct {
		Action     string `json:"action"`
		IDs        []uint `json:"ids"`
		DeleteFile *bool  `json:"deleteFile"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON body"})
	}
	action := strings.ToLower(strings.TrimSpace(body.Action))
	switch action {
	case "pin", "unpin", "delete", "verify":
	default:
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "action must be pin, unpin, delete, or verify"})
	}
	if len(body.IDs) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ids must not be empty"})
	}
	if len(body.IDs) > maxBulkBackupIDs {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "too many ids (max 200)"})
	}
	deleteFile := true
	if body.DeleteFile != nil {
		deleteFile = *body.DeleteFile
	}

	ctx := c.UserContext()
	results := make([]bulkItemResult, 0, len(body.IDs))
	succeeded := 0
	for _, id := range body.IDs {
		res := bulkItemResult{ID: id}
		switch action {
		case "pin", "unpin":
			pinned := action == "pin"
			var logEntry models.BackupLog
			if err := s.db.First(&logEntry, id).Error; err != nil {
				res.Error = "backup log not found"
			} else if logEntry.Status != "success" {
				res.Error = "only successful backups can be pinned"
			} else if err := s.db.Model(&logEntry).Update("pinned", pinned).Error; err != nil {
				res.Error = err.Error()
			} else {
				res.OK = true
				res.Pinned = &pinned
			}
		case "delete":
			if _, err := s.deleteBackupByID(ctx, id, deleteFile); err != nil {
				res.Error = err.Error()
			} else {
				res.OK = true
			}
		case "verify":
			v, _, err := s.verifyBackupByID(ctx, strconv.FormatUint(uint64(id), 10))
			if err != nil {
				res.Error = err.Error()
			} else {
				res.OK = v.Match
				res.Match = &v.Match
				if !v.Match {
					res.Error = "checksum mismatch"
				}
			}
		}
		if res.OK {
			succeeded++
		}
		results = append(results, res)
	}

	return c.JSON(fiber.Map{
		"action":    action,
		"succeeded": succeeded,
		"failed":    len(results) - succeeded,
		"results":   results,
	})
}

type archiveFileInfo struct {
	Size    int64
	ModTime time.Time
}

type compareFile struct {
	Name       string     `json:"name"`
	Size       int64      `json:"size"`
	OldSize    *int64     `json:"oldSize,omitempty"`
	ModTime    time.Time  `json:"modTime"`
	OldModTime *time.Time `json:"oldModTime,omitempty"`
}

type compareSide struct {
	ID         uint      `json:"id"`
	TargetName string    `json:"targetName"`
	FileName   string    `json:"fileName"`
	StartedAt  time.Time `json:"startedAt"`
	Note       string    `json:"note,omitempty"`
	FileCount  int       `json:"fileCount"`
	TotalBytes int64     `json:"totalBytes"`
}

// CompareBackups diffs the file listings of two archives (older "base" vs newer "target").
func (s *Server) CompareBackups(c *fiber.Ctx) error {
	baseID, targetID := strings.TrimSpace(c.Query("base")), strings.TrimSpace(c.Query("target"))
	if baseID == "" || targetID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "base and target query parameters are required"})
	}
	if baseID == targetID {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "select two different backups to compare"})
	}

	ctx := c.UserContext()
	baseLog, baseFiles, status, err := s.readArchiveIndex(ctx, baseID)
	if err != nil {
		return c.Status(status).JSON(fiber.Map{"error": "base: " + err.Error()})
	}
	targetLog, targetFiles, status, err := s.readArchiveIndex(ctx, targetID)
	if err != nil {
		return c.Status(status).JSON(fiber.Map{"error": "target: " + err.Error()})
	}
	// Always present the diff as old -> new.
	if targetLog.StartedAt.Before(baseLog.StartedAt) {
		baseLog, targetLog = targetLog, baseLog
		baseFiles, targetFiles = targetFiles, baseFiles
	}

	var added, removed, changed []compareFile
	unchanged := 0
	var addedBytes, removedBytes int64
	for name, cur := range targetFiles {
		prev, ok := baseFiles[name]
		switch {
		case !ok:
			added = append(added, compareFile{Name: name, Size: cur.Size, ModTime: cur.ModTime})
			addedBytes += cur.Size
		case prev.Size != cur.Size || !prev.ModTime.Equal(cur.ModTime):
			oldSize, oldMod := prev.Size, prev.ModTime
			changed = append(changed, compareFile{Name: name, Size: cur.Size, OldSize: &oldSize, ModTime: cur.ModTime, OldModTime: &oldMod})
		default:
			unchanged++
		}
	}
	for name, prev := range baseFiles {
		if _, ok := targetFiles[name]; !ok {
			removed = append(removed, compareFile{Name: name, Size: prev.Size, ModTime: prev.ModTime})
			removedBytes += prev.Size
		}
	}

	summary := fiber.Map{
		"added":        len(added),
		"removed":      len(removed),
		"changed":      len(changed),
		"unchanged":    unchanged,
		"addedBytes":   addedBytes,
		"removedBytes": removedBytes,
	}
	truncated := len(added) > maxCompareListedDiff || len(removed) > maxCompareListedDiff || len(changed) > maxCompareListedDiff
	return c.JSON(fiber.Map{
		"base":      sideOf(baseLog, baseFiles),
		"target":    sideOf(targetLog, targetFiles),
		"summary":   summary,
		"added":     sortAndCap(added),
		"removed":   sortAndCap(removed),
		"changed":   sortAndCap(changed),
		"truncated": truncated,
	})
}

func sideOf(l models.BackupLog, files map[string]archiveFileInfo) compareSide {
	var total int64
	for _, f := range files {
		total += f.Size
	}
	return compareSide{
		ID: l.ID, TargetName: l.TargetName, FileName: l.FileName, StartedAt: l.StartedAt,
		Note: l.Note, FileCount: len(files), TotalBytes: total,
	}
}

func sortAndCap(items []compareFile) []compareFile {
	if items == nil {
		return []compareFile{}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	if len(items) > maxCompareListedDiff {
		items = items[:maxCompareListedDiff]
	}
	return items
}

// readArchiveIndex streams an archive and returns its non-directory entries keyed by normalized path.
func (s *Server) readArchiveIndex(ctx context.Context, rawID string) (models.BackupLog, map[string]archiveFileInfo, int, error) {
	logEntry, provider, driver, status, errMsg := s.openBackupContext(rawID)
	if status != 0 {
		return logEntry, nil, status, errors.New(errMsg)
	}
	reader, _, err := s.openArchiveStream(ctx, logEntry, provider, driver, true)
	if err != nil {
		return logEntry, nil, httpStatusForArchiveErr(err), err
	}
	defer reader.Close()

	decTar, err := dvcrypto.OpenCompressedReader(reader, logEntry.FileName)
	if err != nil {
		return logEntry, nil, fiber.StatusBadRequest, errors.New("invalid compressed archive: " + err.Error())
	}
	defer decTar.Close()

	files := make(map[string]archiveFileInfo)
	tr := tar.NewReader(decTar)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return logEntry, nil, fiber.StatusBadRequest, errors.New("failed reading tar entries: " + err.Error())
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		key := docker.ArchivePathKey(hdr.Name)
		if key == "" {
			continue
		}
		if len(files) >= maxCompareEntries {
			return logEntry, nil, fiber.StatusRequestEntityTooLarge, errors.New("archive has too many entries to compare")
		}
		files[key] = archiveFileInfo{Size: hdr.Size, ModTime: hdr.ModTime.UTC()}
	}
	return logEntry, files, 0, nil
}
