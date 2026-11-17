package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LocalDriver stores backup archives on a local filesystem directory.
type LocalDriver struct {
	baseDir string
}

// NewLocalDriver initializes a LocalDriver rooted at baseDir.
func NewLocalDriver(baseDir string) (*LocalDriver, error) {
	absBase, err := filepath.Abs(strings.TrimSpace(baseDir))
	if err != nil {
		return nil, fmt.Errorf("resolve local storage path: %w", err)
	}
	return &LocalDriver{baseDir: absBase}, nil
}

func (d *LocalDriver) Test(_ context.Context) error {
	if err := os.MkdirAll(d.baseDir, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", d.baseDir, err)
	}
	probe, err := os.CreateTemp(d.baseDir, ".dockvault-probe-*")
	if err != nil {
		return fmt.Errorf("directory %s is not writable: %w", d.baseDir, err)
	}
	probeName := probe.Name()
	_ = probe.Close()
	_ = os.Remove(probeName)
	return nil
}

func (d *LocalDriver) Save(_ context.Context, localTempPath, fileName string) (string, error) {
	safeName, err := SafeFileName(fileName)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(d.baseDir, 0o755); err != nil {
		return "", fmt.Errorf("create directory %s: %w", d.baseDir, err)
	}

	destPath := filepath.Join(d.baseDir, safeName)
	if !d.isContained(destPath) {
		return "", fmt.Errorf("destination path escapes base directory")
	}

	// Fast path: atomic rename when on the same filesystem.
	if err := os.Rename(localTempPath, destPath); err == nil {
		return destPath, nil
	}

	// Cross-device fallback: stream copy then remove temp file.
	src, err := os.Open(localTempPath)
	if err != nil {
		return "", fmt.Errorf("open temp archive: %w", err)
	}
	defer src.Close()

	dst, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return "", fmt.Errorf("create destination archive: %w", err)
	}

	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		_ = os.Remove(destPath)
		return "", fmt.Errorf("copy archive to local storage: %w", err)
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(destPath)
		return "", fmt.Errorf("close destination archive: %w", err)
	}
	_ = os.Remove(localTempPath)
	return destPath, nil
}

func (d *LocalDriver) Open(_ context.Context, storagePath string) (io.ReadCloser, int64, error) {
	resolved, err := d.resolveExisting(storagePath)
	if err != nil {
		return nil, 0, err
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, 0, fmt.Errorf("open backup file: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, 0, fmt.Errorf("stat backup file: %w", err)
	}
	return f, info.Size(), nil
}

func (d *LocalDriver) Delete(_ context.Context, storagePath string) error {
	resolved, err := d.resolveExisting(storagePath)
	if err != nil {
		return err
	}
	if err := os.Remove(resolved); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove local backup: %w", err)
	}
	return nil
}

// List enumerates backup archives in the local directory.
func (d *LocalDriver) List(_ context.Context) ([]RemoteObject, error) {
	entries, err := os.ReadDir(d.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list local backups: %w", err)
	}

	var out []RemoteObject
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".tar.gz") && !strings.HasSuffix(name, ".tar.gz.enc") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		out = append(out, RemoteObject{
			Name:         name,
			StoragePath:  filepath.Join(d.baseDir, name),
			SizeBytes:    info.Size(),
			LastModified: info.ModTime().UTC(),
			Encrypted:    strings.HasSuffix(name, ".enc"),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].LastModified.After(out[j].LastModified)
	})
	return out, nil
}

func (d *LocalDriver) resolveExisting(storagePath string) (string, error) {
	// Always restrict access to files within d.baseDir using the base filename if needed.
	candidate := filepath.Clean(storagePath)
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(d.baseDir, candidate)
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve backup path: %w", err)
	}
	if !d.isContained(absCandidate) {
		return "", fmt.Errorf("access denied: path outside storage directory")
	}
	return absCandidate, nil
}

func (d *LocalDriver) isContained(target string) bool {
	rel, err := filepath.Rel(d.baseDir, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
