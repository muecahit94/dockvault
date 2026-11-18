package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/muecahit94/dockvault/models"
	"github.com/willscott/go-nfs-client/nfs"
	"github.com/willscott/go-nfs-client/nfs/rpc"
)

// NFSDriver stores backup archives on a remote NFSv3 export without requiring kernel mounts or root privileges.
type NFSDriver struct {
	host       string
	exportPath string
	subDir     string
	uid        int
	gid        int
}

// NewNFSDriver initializes an NFSDriver from a StorageProvider configuration.
func NewNFSDriver(provider models.StorageProvider) (*NFSDriver, error) {
	host, _, err := parseHostPort(provider.Host, provider.Port, 2049)
	if err != nil {
		return nil, fmt.Errorf("NFS host: %w", err)
	}

	rawExport := strings.TrimSpace(strings.ReplaceAll(provider.NFSExport, `\`, `/`))
	if rawExport == "" {
		return nil, fmt.Errorf("NFS export path is required")
	}
	if !strings.HasPrefix(rawExport, "/") {
		rawExport = "/" + rawExport
	}
	rawExport = path.Clean(rawExport)

	rawSubdir := strings.TrimSpace(provider.RemotePath)
	if rawSubdir == "" {
		rawSubdir = strings.TrimSpace(provider.Prefix)
	}
	subDir, err := cleanRemoteSubdir(rawSubdir)
	if err != nil {
		return nil, err
	}

	uid := provider.NFSUID
	if uid < 0 {
		uid = 0
	}
	gid := provider.NFSGID
	if gid < 0 {
		gid = 0
	}

	return &NFSDriver{
		host:       host,
		exportPath: rawExport,
		subDir:     subDir,
		uid:        uid,
		gid:        gid,
	}, nil
}

func (d *NFSDriver) mount(_ context.Context) (*nfs.Target, func(), error) {
	mnt, err := nfs.DialMount(d.host, 5*time.Second)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to NFS mountd on %s (ensure portmapper 111 is reachable): %w", d.host, err)
	}

	auth := rpc.NewAuthUnix("dockvault", uint32(d.uid), uint32(d.gid)).Auth()
	target, err := mnt.Mount(d.exportPath, auth)
	if err != nil {
		mnt.Close()
		return nil, nil, fmt.Errorf("mount NFS export %s:%s: %w", d.host, d.exportPath, err)
	}

	cleanup := func() {
		target.Close()
		_ = mnt.Unmount()
		mnt.Close()
	}
	return target, cleanup, nil
}

func (d *NFSDriver) remotePath(name string) string {
	if d.subDir == "" {
		return name
	}
	return path.Join(d.subDir, name)
}

func mkdirAllNFS(target *nfs.Target, dir string) error {
	if dir == "" || dir == "." {
		return nil
	}
	cur := ""
	for _, seg := range strings.Split(dir, "/") {
		if seg == "" || seg == "." {
			continue
		}
		if cur == "" {
			cur = seg
		} else {
			cur = path.Join(cur, seg)
		}
		if _, _, err := target.Lookup(cur); err != nil {
			if !os.IsNotExist(err) {
				return fmt.Errorf("lookup NFS directory %q: %w", cur, err)
			}
			if _, mkErr := target.Mkdir(cur, 0o755); mkErr != nil {
				return fmt.Errorf("create NFS directory %q: %w", cur, mkErr)
			}
		}
	}
	return nil
}

func (d *NFSDriver) Test(ctx context.Context) error {
	_, err := d.TestWithDetails(ctx)
	return err
}

// TestWithDetails verifies NFS mount connectivity, directory writability, and lists archive counts.
func (d *NFSDriver) TestWithDetails(ctx context.Context) (*TestResult, error) {
	target, cleanup, err := d.mount(ctx)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	if err := mkdirAllNFS(target, d.subDir); err != nil {
		return nil, err
	}

	probePath := d.remotePath(fmt.Sprintf(".dockvault-probe-%d", time.Now().UnixNano()))
	probeFile, err := target.OpenFile(probePath, 0o640)
	if err != nil {
		return nil, fmt.Errorf("NFS export %s:%s is not writable: %w", d.host, d.exportPath, err)
	}
	_ = probeFile.Close()
	_ = target.Remove(probePath)

	listDir := "."
	if d.subDir != "" {
		listDir = d.subDir
	}
	entries, err := target.ReadDirPlus(listDir)
	if err != nil {
		return nil, fmt.Errorf("list NFS directory %q: %w", listDir, err)
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

	uri := fmt.Sprintf("nfs://%s%s", d.host, d.exportPath)
	if d.subDir != "" {
		uri = path.Join(uri, d.subDir)
	}
	return &TestResult{
		Message:     "Connected to " + uri,
		ObjectCount: &count,
		TotalBytes:  &totalBytes,
	}, nil
}

func (d *NFSDriver) Save(ctx context.Context, localTempPath, fileName string) (string, error) {
	safeName, err := SafeFileName(fileName)
	if err != nil {
		return "", err
	}

	target, cleanup, err := d.mount(ctx)
	if err != nil {
		return "", err
	}
	defer cleanup()

	if err := mkdirAllNFS(target, d.subDir); err != nil {
		return "", err
	}

	destPath := d.remotePath(safeName)
	tmpPath := destPath + ".tmp"
	_ = target.Remove(tmpPath)

	src, err := os.Open(localTempPath)
	if err != nil {
		return "", fmt.Errorf("open temp archive: %w", err)
	}
	defer src.Close()

	dst, err := target.OpenFile(tmpPath, 0o640)
	if err != nil {
		return "", fmt.Errorf("create NFS file %q: %w", tmpPath, err)
	}

	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		_ = target.Remove(tmpPath)
		return "", fmt.Errorf("upload archive to NFS: %w", err)
	}
	if err := dst.Close(); err != nil {
		_ = target.Remove(tmpPath)
		return "", fmt.Errorf("close NFS file: %w", err)
	}

	if err := target.Rename(tmpPath, destPath); err != nil {
		_ = target.Remove(destPath)
		if retryErr := target.Rename(tmpPath, destPath); retryErr != nil {
			_ = target.Remove(tmpPath)
			return "", fmt.Errorf("finalize NFS file %q: %w", destPath, retryErr)
		}
	}

	_ = os.Remove(localTempPath)
	return destPath, nil
}

func (d *NFSDriver) Open(ctx context.Context, storagePath string) (io.ReadCloser, int64, error) {
	cleanPath, err := cleanRemoteSubdir(storagePath)
	if err != nil || cleanPath == "" {
		return nil, 0, fmt.Errorf("invalid NFS storage path %q", storagePath)
	}

	target, cleanup, err := d.mount(ctx)
	if err != nil {
		return nil, 0, err
	}

	info, _, err := target.Lookup(cleanPath)
	if err != nil {
		cleanup()
		return nil, 0, fmt.Errorf("stat NFS file %q: %w", cleanPath, err)
	}
	f, err := target.Open(cleanPath)
	if err != nil {
		cleanup()
		return nil, 0, fmt.Errorf("open NFS file %q: %w", cleanPath, err)
	}
	return &wrappedReadCloser{ReadCloser: f, cleanup: cleanup}, info.Size(), nil
}

func (d *NFSDriver) Delete(ctx context.Context, storagePath string) error {
	cleanPath, err := cleanRemoteSubdir(storagePath)
	if err != nil || cleanPath == "" {
		return nil
	}

	target, cleanup, err := d.mount(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := target.Remove(cleanPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete NFS file %q: %w", cleanPath, err)
	}
	return nil
}

// List enumerates backup archives in the configured NFS export directory.
func (d *NFSDriver) List(ctx context.Context) ([]RemoteObject, error) {
	target, cleanup, err := d.mount(ctx)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	listDir := "."
	if d.subDir != "" {
		listDir = d.subDir
	}
	entries, err := target.ReadDirPlus(listDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list NFS directory %q: %w", listDir, err)
	}

	objects := make([]RemoteObject, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !IsBackupArchiveName(entry.Name()) {
			continue
		}
		objects = append(objects, RemoteObject{
			Name:         entry.Name(),
			StoragePath:  d.remotePath(entry.Name()),
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
