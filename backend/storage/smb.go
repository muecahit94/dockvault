package storage

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/hirochachacha/go-smb2"
	"github.com/muecahit94/dockvault/models"
)

// SMBDriver stores backup archives on an SMB/CIFS file share (Synology, TrueNAS, Samba, Windows).
type SMBDriver struct {
	host      string
	port      int
	shareName string
	subDir    string
	username  string
	password  string
	domain    string
}

// NewSMBDriver initializes an SMBDriver from a StorageProvider configuration.
func NewSMBDriver(provider models.StorageProvider) (*SMBDriver, error) {
	host, port, err := parseHostPort(provider.Host, provider.Port, 445)
	if err != nil {
		return nil, fmt.Errorf("SMB host: %w", err)
	}

	share := normalizeSMBShare(provider.SMBShare)
	if share == "" {
		return nil, fmt.Errorf("SMB share name is required")
	}

	rawSubdir := strings.TrimSpace(provider.RemotePath)
	if rawSubdir == "" {
		rawSubdir = strings.TrimSpace(provider.Prefix)
	}
	subDir, err := cleanRemoteSubdir(rawSubdir)
	if err != nil {
		return nil, err
	}

	return &SMBDriver{
		host:      host,
		port:      port,
		shareName: share,
		subDir:    subDir,
		username:  strings.TrimSpace(provider.Username),
		password:  provider.Password,
		domain:    strings.TrimSpace(provider.SMBDomain),
	}, nil
}

func normalizeSMBShare(raw string) string {
	s := strings.TrimSpace(strings.ReplaceAll(raw, `\`, `/`))
	s = strings.TrimPrefix(s, "//")
	// Handle accidental UNC paste like "//nas.local/backups" -> take share segment if host repeated.
	parts := strings.Split(strings.Trim(s, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	if len(parts) >= 2 && (strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":")) {
		return parts[1]
	}
	return parts[0]
}

func toSMBPath(posixPath string) string {
	cleaned := strings.Trim(strings.ReplaceAll(posixPath, `\`, `/`), "/")
	if cleaned == "" || cleaned == "." {
		return ""
	}
	return strings.ReplaceAll(cleaned, "/", `\`)
}

func (d *SMBDriver) mount(ctx context.Context) (*smb2.Share, func(), error) {
	addr := net.JoinHostPort(d.host, fmt.Sprintf("%d", d.port))
	conn, err := (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to SMB %s: %w", addr, err)
	}

	dialer := &smb2.Dialer{
		Initiator: &smb2.NTLMInitiator{
			User:     d.username,
			Password: d.password,
			Domain:   d.domain,
		},
	}
	session, err := dialer.DialContext(ctx, conn)
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("SMB authenticate on %s: %w", addr, err)
	}
	share, err := session.Mount(d.shareName)
	if err != nil {
		_ = session.Logoff()
		_ = conn.Close()
		return nil, nil, fmt.Errorf("mount SMB share %q: %w", d.shareName, err)
	}

	cleanup := func() {
		_ = share.Umount()
		_ = session.Logoff()
		_ = conn.Close()
	}
	return share, cleanup, nil
}

func (d *SMBDriver) posixPath(fileName string) string {
	if d.subDir == "" {
		return fileName
	}
	return path.Join(d.subDir, fileName)
}

func (d *SMBDriver) ensureSubdir(share *smb2.Share) error {
	if d.subDir == "" {
		return nil
	}
	if err := share.MkdirAll(toSMBPath(d.subDir), 0o755); err != nil {
		return fmt.Errorf("create SMB directory %q: %w", d.subDir, err)
	}
	return nil
}

func (d *SMBDriver) Test(ctx context.Context) error {
	_, err := d.TestWithDetails(ctx)
	return err
}

// TestWithDetails verifies SMB share access, writability, and enumerates backup archives.
func (d *SMBDriver) TestWithDetails(ctx context.Context) (*TestResult, error) {
	share, cleanup, err := d.mount(ctx)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	if err := d.ensureSubdir(share); err != nil {
		return nil, err
	}

	probePosix := d.posixPath(fmt.Sprintf(".dockvault-probe-%d", time.Now().UnixNano()))
	probeSMB := toSMBPath(probePosix)
	f, err := share.OpenFile(probeSMB, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return nil, fmt.Errorf("SMB share %q is not writable: %w", d.shareName, err)
	}
	_ = f.Close()
	_ = share.Remove(probeSMB)

	entries, err := share.ReadDir(toSMBPath(d.subDir))
	if err != nil {
		return nil, fmt.Errorf("list SMB share directory: %w", err)
	}

	count := 0
	var totalBytes int64
	for _, entry := range entries {
		if entry.IsDir() || !IsBackupArchiveName(entry.Name()) {
			continue
		}
		count++
		totalBytes += entry.Size()
	}

	targetURI := fmt.Sprintf("smb://%s/%s", d.host, d.shareName)
	if d.subDir != "" {
		targetURI += "/" + d.subDir
	}
	return &TestResult{
		Message:     "Connected to " + targetURI,
		ObjectCount: &count,
		TotalBytes:  &totalBytes,
	}, nil
}

func (d *SMBDriver) Save(ctx context.Context, localTempPath, fileName string) (string, error) {
	safeName, err := SafeFileName(fileName)
	if err != nil {
		return "", err
	}

	share, cleanup, err := d.mount(ctx)
	if err != nil {
		return "", err
	}
	defer cleanup()

	if err := d.ensureSubdir(share); err != nil {
		return "", err
	}

	destPosix := d.posixPath(safeName)
	tmpPosix := destPosix + ".tmp"
	destSMB := toSMBPath(destPosix)
	tmpSMB := toSMBPath(tmpPosix)

	src, err := os.Open(localTempPath)
	if err != nil {
		return "", fmt.Errorf("open temp archive: %w", err)
	}
	defer src.Close()

	dst, err := share.OpenFile(tmpSMB, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return "", fmt.Errorf("create SMB file %q: %w", destPosix, err)
	}

	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		_ = share.Remove(tmpSMB)
		return "", fmt.Errorf("upload archive to SMB: %w", err)
	}
	if err := dst.Close(); err != nil {
		_ = share.Remove(tmpSMB)
		return "", fmt.Errorf("close SMB file: %w", err)
	}

	if err := share.Rename(tmpSMB, destSMB); err != nil {
		_ = share.Remove(destSMB)
		if retryErr := share.Rename(tmpSMB, destSMB); retryErr != nil {
			_ = share.Remove(tmpSMB)
			return "", fmt.Errorf("finalize SMB file %q: %w", destPosix, retryErr)
		}
	}

	_ = os.Remove(localTempPath)
	return destPosix, nil
}

func (d *SMBDriver) Open(ctx context.Context, storagePath string) (io.ReadCloser, int64, error) {
	cleanPath, err := cleanRemoteSubdir(storagePath)
	if err != nil || cleanPath == "" {
		return nil, 0, fmt.Errorf("invalid SMB storage path %q", storagePath)
	}

	share, cleanup, err := d.mount(ctx)
	if err != nil {
		return nil, 0, err
	}

	smbPath := toSMBPath(cleanPath)
	f, err := share.Open(smbPath)
	if err != nil {
		cleanup()
		return nil, 0, fmt.Errorf("open SMB file %q: %w", cleanPath, err)
	}
	stat, err := f.Stat()
	if err != nil {
		_ = f.Close()
		cleanup()
		return nil, 0, fmt.Errorf("stat SMB file %q: %w", cleanPath, err)
	}
	return &wrappedReadCloser{ReadCloser: f, cleanup: cleanup}, stat.Size(), nil
}

func (d *SMBDriver) Delete(ctx context.Context, storagePath string) error {
	cleanPath, err := cleanRemoteSubdir(storagePath)
	if err != nil || cleanPath == "" {
		return nil
	}

	share, cleanup, err := d.mount(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := share.Remove(toSMBPath(cleanPath)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete SMB file %q: %w", cleanPath, err)
	}
	return nil
}

// List enumerates backup archives in the configured SMB share subdirectory.
func (d *SMBDriver) List(ctx context.Context) ([]RemoteObject, error) {
	share, cleanup, err := d.mount(ctx)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	entries, err := share.ReadDir(toSMBPath(d.subDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list SMB directory: %w", err)
	}

	objects := make([]RemoteObject, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !IsBackupArchiveName(entry.Name()) {
			continue
		}
		objects = append(objects, RemoteObject{
			Name:         entry.Name(),
			StoragePath:  d.posixPath(entry.Name()),
			SizeBytes:    entry.Size(),
			LastModified: entry.ModTime().UTC(),
			Encrypted:    strings.HasSuffix(entry.Name(), ".enc"),
		})
	}
	sort.Slice(objects, func(i, j int) bool {
		return objects[i].LastModified.After(objects[j].LastModified)
	})
	return objects, nil
}
