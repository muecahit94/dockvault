package storage

import (
	"context"
	"fmt"
	"io"
	"net"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/muecahit94/dockvault/models"
)

// TestResult holds optional diagnostics returned by a storage connectivity test.
type TestResult struct {
	Message          string `json:"message,omitempty"`
	ContainerCreated bool   `json:"containerCreated,omitempty"`
	ObjectCount      *int   `json:"objectCount,omitempty"`
	TotalBytes       *int64 `json:"totalBytes,omitempty"`
}

// Driver abstracts backup storage operations across local disk, S3, and Azure Blob Storage.
type Driver interface {
	Test(ctx context.Context) error
	Save(ctx context.Context, localTempPath, fileName string) (storagePath string, err error)
	Open(ctx context.Context, storagePath string) (io.ReadCloser, int64, error)
	Delete(ctx context.Context, storagePath string) error
}

// DetailedTester is an optional interface for drivers that report rich test diagnostics.
type DetailedTester interface {
	TestWithDetails(ctx context.Context) (*TestResult, error)
}

// RemoteObject represents an archive file discovered on a storage backend during disaster-recovery scan.
type RemoteObject struct {
	Name         string    `json:"name"`
	StoragePath  string    `json:"storagePath"`
	SizeBytes    int64     `json:"sizeBytes"`
	LastModified time.Time `json:"lastModified"`
	Encrypted    bool      `json:"encrypted"`
}

// ObjectLister is implemented by storage backends that can enumerate stored backup archives.
type ObjectLister interface {
	List(ctx context.Context) ([]RemoteObject, error)
}

// SASURLGenerator is an optional interface for drivers supporting pre-signed time-limited download URLs.
type SASURLGenerator interface {
	GenerateSASURL(ctx context.Context, storagePath string, ttl time.Duration) (string, error)
}

// NewDriver instantiates the appropriate storage Driver for a StorageProvider record.
func NewDriver(provider models.StorageProvider, defaultBackupDir string) (Driver, error) {
	switch provider.Type {
	case "local":
		dir := strings.TrimSpace(provider.LocalPath)
		if dir == "" {
			dir = defaultBackupDir
		}
		return NewLocalDriver(dir)
	case "s3":
		return NewS3Driver(provider)
	case "azure":
		return NewAzureDriver(provider)
	case "ssh":
		return NewSSHDriver(provider)
	case "smb":
		return NewSMBDriver(provider)
	case "nfs":
		return NewNFSDriver(provider)
	default:
		return nil, fmt.Errorf("unsupported storage provider type %q", provider.Type)
	}
}

// wrappedReadCloser closes the underlying stream and tears down remote session handles once.
type wrappedReadCloser struct {
	io.ReadCloser
	once    sync.Once
	cleanup func()
}

func (w *wrappedReadCloser) Close() error {
	err := w.ReadCloser.Close()
	w.once.Do(func() {
		if w.cleanup != nil {
			w.cleanup()
		}
	})
	return err
}

func parseHostPort(rawHost string, explicitPort, defaultPort int) (string, int, error) {
	host := strings.TrimSpace(rawHost)
	if host == "" {
		return "", 0, fmt.Errorf("host is required")
	}
	port := explicitPort
	if h, pStr, err := net.SplitHostPort(host); err == nil {
		host = strings.TrimSpace(h)
		if port <= 0 {
			parsed, convErr := strconv.Atoi(pStr)
			if convErr != nil || parsed <= 0 || parsed > 65535 {
				return "", 0, fmt.Errorf("invalid port %q", pStr)
			}
			port = parsed
		}
	} else {
		host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	}
	if port <= 0 {
		port = defaultPort
	}
	if port > 65535 {
		return "", 0, fmt.Errorf("invalid port %d", port)
	}
	return host, port, nil
}

func cleanRemoteSubdir(raw string) (string, error) {
	trimmed := strings.TrimSpace(strings.ReplaceAll(raw, `\`, `/`))
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" || trimmed == "." {
		return "", nil
	}
	for _, seg := range strings.Split(trimmed, "/") {
		if seg == ".." {
			return "", fmt.Errorf("remote path %q must not contain '..'", raw)
		}
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("invalid remote path %q", raw)
	}
	return cleaned, nil
}

// SafeFileName strips directory components and rejects traversal attempts.
func SafeFileName(name string) (string, error) {
	cleaned := filepath.Base(strings.TrimSpace(name))
	if cleaned == "" || cleaned == "." || cleaned == ".." || strings.ContainsAny(cleaned, `/\`) {
		return "", fmt.Errorf("invalid backup filename %q", name)
	}
	return cleaned, nil
}

// IsBackupArchiveName reports whether a filename matches a DockVault archive (.tar.gz/.tar.zst, optionally .enc).
func IsBackupArchiveName(name string) bool {
	return strings.HasSuffix(name, ".tar.gz") ||
		strings.HasSuffix(name, ".tar.gz.enc") ||
		strings.HasSuffix(name, ".tar.zst") ||
		strings.HasSuffix(name, ".tar.zst.enc")
}
