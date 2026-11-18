package storage

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/muecahit94/dockvault/models"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SSHDriver stores backup archives on a remote host via SSH/SFTP.
type SSHDriver struct {
	host        string
	port        int
	username    string
	password    string
	useLocalKey bool
	keyPath     string
	privateKey  string
	passphrase  string
	baseDir     string
}

// NewSSHDriver initializes an SSHDriver from a StorageProvider configuration.
func NewSSHDriver(provider models.StorageProvider) (*SSHDriver, error) {
	host, port, err := parseHostPort(provider.Host, provider.Port, 22)
	if err != nil {
		return nil, fmt.Errorf("SSH host: %w", err)
	}
	username := strings.TrimSpace(provider.Username)
	if username == "" {
		return nil, fmt.Errorf("SSH username is required")
	}
	password := provider.Password
	privateKey := strings.TrimSpace(provider.SSHPrivateKey)
	keyPath := strings.TrimSpace(provider.SSHKeyPath)
	useLocalKey := provider.SSHUseLocalKey || keyPath != ""
	if password == "" && privateKey == "" && !useLocalKey {
		return nil, fmt.Errorf("SSH password, private key, or container local SSH key is required")
	}

	rawDir := strings.TrimSpace(provider.RemotePath)
	if rawDir == "" {
		rawDir = strings.TrimSpace(provider.Prefix)
	}
	baseDir, err := cleanSSHBaseDir(rawDir)
	if err != nil {
		return nil, err
	}

	drv := &SSHDriver{
		host:        host,
		port:        port,
		username:    username,
		password:    password,
		useLocalKey: useLocalKey,
		keyPath:     keyPath,
		privateKey:  privateKey,
		passphrase:  provider.SSHKeyPassphrase,
		baseDir:     baseDir,
	}
	if _, err := drv.buildAuthMethods(); err != nil {
		return nil, err
	}
	return drv, nil
}

func cleanSSHBaseDir(raw string) (string, error) {
	p := strings.TrimSpace(strings.ReplaceAll(raw, `\`, `/`))
	if p == "" || p == "." {
		return ".", nil
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", fmt.Errorf("SSH remote path %q must not contain '..'", raw)
		}
	}
	cleaned := path.Clean(p)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("invalid SSH remote path %q", raw)
	}
	return cleaned, nil
}

func parseSSHSigner(pemBytes []byte, passphrase string) (ssh.Signer, error) {
	if passphrase != "" {
		return ssh.ParsePrivateKeyWithPassphrase(pemBytes, []byte(passphrase))
	}
	return ssh.ParsePrivateKey(pemBytes)
}

func readLocalSSHKey(explicitPath string) ([]byte, string, error) {
	if p := strings.TrimSpace(explicitPath); p != "" {
		if strings.HasPrefix(p, "~/") {
			if home, err := os.UserHomeDir(); err == nil && home != "" {
				p = filepath.Join(home, strings.TrimPrefix(p, "~/"))
			}
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, "", fmt.Errorf("read local SSH key %q: %w", explicitPath, err)
		}
		return raw, p, nil
	}

	candidates := make([]string, 0, 6)
	seen := make(map[string]bool)
	addDir := func(dir string) {
		if dir == "" {
			return
		}
		for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
			p := filepath.Join(dir, name)
			if !seen[p] {
				seen[p] = true
				candidates = append(candidates, p)
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		addDir(filepath.Join(home, ".ssh"))
	}
	addDir("/root/.ssh")

	for _, p := range candidates {
		if raw, err := os.ReadFile(p); err == nil && len(raw) > 0 {
			return raw, p, nil
		}
	}
	return nil, "", fmt.Errorf("no readable SSH private key found in container (checked %s); mount a key into /root/.ssh or set SSH Key Path", strings.Join(candidates, ", "))
}

func (d *SSHDriver) buildAuthMethods() ([]ssh.AuthMethod, error) {
	methods := make([]ssh.AuthMethod, 0, 4)
	if d.useLocalKey {
		raw, resolvedPath, err := readLocalSSHKey(d.keyPath)
		if err != nil {
			return nil, err
		}
		signer, err := parseSSHSigner(raw, d.passphrase)
		if err != nil {
			return nil, fmt.Errorf("parse local SSH private key %q: %w", resolvedPath, err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if d.privateKey != "" {
		signer, err := parseSSHSigner([]byte(d.privateKey), d.passphrase)
		if err != nil {
			return nil, fmt.Errorf("parse SSH private key: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if d.password != "" {
		pw := d.password
		methods = append(methods,
			ssh.Password(pw),
			ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range questions {
					answers[i] = pw
				}
				return answers, nil
			}),
		)
	}
	return methods, nil
}

func (d *SSHDriver) dial(ctx context.Context) (*sftp.Client, func(), error) {
	methods, err := d.buildAuthMethods()
	if err != nil {
		return nil, nil, err
	}
	addr := net.JoinHostPort(d.host, fmt.Sprintf("%d", d.port))
	cfg := &ssh.ClientConfig{
		User:            d.username,
		Auth:            methods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}

	rawConn, err := (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to SSH %s: %w", addr, err)
	}
	c, chans, reqs, err := ssh.NewClientConn(rawConn, addr, cfg)
	if err != nil {
		_ = rawConn.Close()
		return nil, nil, fmt.Errorf("SSH handshake with %s: %w", addr, err)
	}
	sshClient := ssh.NewClient(c, chans, reqs)
	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		_ = sshClient.Close()
		return nil, nil, fmt.Errorf("start SFTP subsystem on %s: %w", addr, err)
	}

	cleanup := func() {
		_ = sftpClient.Close()
		_ = sshClient.Close()
	}
	return sftpClient, cleanup, nil
}

func (d *SSHDriver) remotePath(name string) string {
	if d.baseDir == "" || d.baseDir == "." {
		return name
	}
	return path.Join(d.baseDir, name)
}

func (d *SSHDriver) ensureBaseDir(client *sftp.Client) error {
	if d.baseDir == "" || d.baseDir == "." {
		return nil
	}
	if err := client.MkdirAll(d.baseDir); err != nil {
		return fmt.Errorf("ensure remote SSH directory %q: %w", d.baseDir, err)
	}
	return nil
}

func (d *SSHDriver) Test(ctx context.Context) error {
	_, err := d.TestWithDetails(ctx)
	return err
}

// TestWithDetails verifies SSH connectivity, directory writability, and reports archive counts.
func (d *SSHDriver) TestWithDetails(ctx context.Context) (*TestResult, error) {
	client, cleanup, err := d.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	if err := d.ensureBaseDir(client); err != nil {
		return nil, err
	}

	probe := d.remotePath(fmt.Sprintf(".dockvault-probe-%d", time.Now().UnixNano()))
	f, err := client.Create(probe)
	if err != nil {
		return nil, fmt.Errorf("SSH directory %q is not writable: %w", d.baseDir, err)
	}
	_ = f.Close()
	_ = client.Remove(probe)

	entries, err := client.ReadDir(d.baseDir)
	if err != nil {
		return nil, fmt.Errorf("list SSH directory %q: %w", d.baseDir, err)
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

	return &TestResult{
		Message:     fmt.Sprintf("Connected to SSH sftp://%s:%d/%s", d.host, d.port, strings.TrimPrefix(d.baseDir, "/")),
		ObjectCount: &count,
		TotalBytes:  &totalBytes,
	}, nil
}

func (d *SSHDriver) Save(ctx context.Context, localTempPath, fileName string) (string, error) {
	safeName, err := SafeFileName(fileName)
	if err != nil {
		return "", err
	}

	client, cleanup, err := d.dial(ctx)
	if err != nil {
		return "", err
	}
	defer cleanup()

	if err := d.ensureBaseDir(client); err != nil {
		return "", err
	}

	destPath := d.remotePath(safeName)
	tmpPath := destPath + ".tmp"

	src, err := os.Open(localTempPath)
	if err != nil {
		return "", fmt.Errorf("open temp archive: %w", err)
	}
	defer src.Close()

	dst, err := client.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC)
	if err != nil {
		return "", fmt.Errorf("create remote SSH file %q: %w", tmpPath, err)
	}

	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		_ = client.Remove(tmpPath)
		return "", fmt.Errorf("upload archive via SSH: %w", err)
	}
	if err := dst.Close(); err != nil {
		_ = client.Remove(tmpPath)
		return "", fmt.Errorf("close remote SSH file: %w", err)
	}

	if err := client.PosixRename(tmpPath, destPath); err != nil {
		_ = client.Remove(destPath)
		if renameErr := client.Rename(tmpPath, destPath); renameErr != nil {
			_ = client.Remove(tmpPath)
			return "", fmt.Errorf("finalize remote SSH file %q: %w", destPath, renameErr)
		}
	}

	_ = os.Remove(localTempPath)
	return destPath, nil
}

func (d *SSHDriver) Open(ctx context.Context, storagePath string) (io.ReadCloser, int64, error) {
	remoteFile := strings.TrimSpace(storagePath)
	if remoteFile == "" || strings.Contains(remoteFile, "..") {
		return nil, 0, fmt.Errorf("invalid SSH storage path %q", storagePath)
	}

	client, cleanup, err := d.dial(ctx)
	if err != nil {
		return nil, 0, err
	}

	f, err := client.Open(remoteFile)
	if err != nil {
		cleanup()
		return nil, 0, fmt.Errorf("open remote SSH file %q: %w", remoteFile, err)
	}
	stat, err := f.Stat()
	if err != nil {
		_ = f.Close()
		cleanup()
		return nil, 0, fmt.Errorf("stat remote SSH file %q: %w", remoteFile, err)
	}
	return &wrappedReadCloser{ReadCloser: f, cleanup: cleanup}, stat.Size(), nil
}

func (d *SSHDriver) Delete(ctx context.Context, storagePath string) error {
	remoteFile := strings.TrimSpace(storagePath)
	if remoteFile == "" || strings.Contains(remoteFile, "..") {
		return nil
	}

	client, cleanup, err := d.dial(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := client.Remove(remoteFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete remote SSH file %q: %w", remoteFile, err)
	}
	return nil
}

// List enumerates backup archives in the configured remote SSH directory.
func (d *SSHDriver) List(ctx context.Context) ([]RemoteObject, error) {
	client, cleanup, err := d.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	entries, err := client.ReadDir(d.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list SSH directory %q: %w", d.baseDir, err)
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
