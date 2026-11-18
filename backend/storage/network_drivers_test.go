package storage

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muecahit94/dockvault/models"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func startTestSFTPServer(t *testing.T, rootDir, username, password string, allowedPubKey ssh.PublicKey) (string, int) {
	t.Helper()

	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatalf("host signer: %v", err)
	}

	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == username && string(pass) == password {
				return nil, nil
			}
			return nil, fmt.Errorf("invalid credentials")
		},
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if c.User() == username && allowedPubKey != nil && string(key.Marshal()) == string(allowedPubKey.Marshal()) {
				return nil, nil
			}
			return nil, fmt.Errorf("unauthorized key")
		},
	}
	cfg.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen sftp test server: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(nc net.Conn) {
				defer nc.Close()
				_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for newChan := range chans {
					if newChan.ChannelType() != "session" {
						_ = newChan.Reject(ssh.UnknownChannelType, "unknown channel")
						continue
					}
					ch, chReqs, err := newChan.Accept()
					if err != nil {
						continue
					}
					go func(in <-chan *ssh.Request) {
						for req := range in {
							ok := req.Type == "subsystem" && len(req.Payload) >= 4 && string(req.Payload[4:]) == "sftp"
							if req.WantReply {
								_ = req.Reply(ok, nil)
							}
							if ok {
								srv, err := sftp.NewServer(ch, sftp.WithServerWorkingDirectory(rootDir))
								if err == nil {
									_ = srv.Serve()
									_ = srv.Close()
								}
								return
							}
						}
					}(chReqs)
				}
			}(conn)
		}
	}()

	tcpAddr := ln.Addr().(*net.TCPAddr)
	return tcpAddr.IP.String(), tcpAddr.Port
}

func TestSSHDriverRoundTripKeyAndPassword(t *testing.T) {
	remoteRoot := t.TempDir()

	// Generate an OpenSSH ed25519 private key in memory.
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate client key: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("ssh public key: %v", err)
	}
	pemBlock, err := ssh.MarshalPrivateKey(priv, "test-key")
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	privPEM := string(pem.EncodeToMemory(pemBlock))

	host, port := startTestSFTPServer(t, remoteRoot, "backupuser", "s3cr3t", sshPub)

	// Test key-authenticated driver under nested subdirectory.
	provider := models.StorageProvider{
		Name:          "Homelab NAS SFTP",
		Type:          "ssh",
		Host:          host,
		Port:          port,
		Username:      "backupuser",
		SSHPrivateKey: privPEM,
		RemotePath:    "homelab/vault",
	}
	drv, err := NewDriver(provider, t.TempDir())
	if err != nil {
		t.Fatalf("NewDriver ssh: %v", err)
	}

	ctx := context.Background()
	details, err := drv.(DetailedTester).TestWithDetails(ctx)
	if err != nil {
		t.Fatalf("TestWithDetails ssh: %v", err)
	}
	if details.ObjectCount == nil || *details.ObjectCount != 0 || !strings.Contains(details.Message, "sftp://") {
		t.Fatalf("unexpected SSH test details: %+v", details)
	}

	localTmp := filepath.Join(t.TempDir(), "upload.tar.gz")
	payload := "dockvault-ssh-archive-bytes"
	if err := os.WriteFile(localTmp, []byte(payload), 0o644); err != nil {
		t.Fatalf("write temp: %v", err)
	}

	storagePath, err := drv.Save(ctx, localTmp, "app-20261118-120000.tar.gz")
	if err != nil {
		t.Fatalf("Save ssh: %v", err)
	}
	if storagePath != "homelab/vault/app-20261118-120000.tar.gz" {
		t.Fatalf("unexpected storagePath %q", storagePath)
	}

	rc, size, err := drv.Open(ctx, storagePath)
	if err != nil {
		t.Fatalf("Open ssh: %v", err)
	}
	gotBytes, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil || size != int64(len(payload)) || string(gotBytes) != payload {
		t.Fatalf("Open read mismatch: err=%v size=%d got=%q", err, size, string(gotBytes))
	}

	listed, err := drv.(ObjectLister).List(ctx)
	if err != nil || len(listed) != 1 || listed[0].Name != "app-20261118-120000.tar.gz" {
		t.Fatalf("List ssh mismatch: err=%v listed=%+v", err, listed)
	}

	if err := drv.Delete(ctx, storagePath); err != nil {
		t.Fatalf("Delete ssh: %v", err)
	}
	listedAfter, err := drv.(ObjectLister).List(ctx)
	if err != nil || len(listedAfter) != 0 {
		t.Fatalf("expected empty list after Delete, got err=%v listed=%+v", err, listedAfter)
	}

	// Verify password-based driver also connects cleanly.
	pwDrv, err := NewSSHDriver(models.StorageProvider{
		Type:       "ssh",
		Host:       host,
		Port:       port,
		Username:   "backupuser",
		Password:   "s3cr3t",
		RemotePath: "homelab/vault",
	})
	if err != nil {
		t.Fatalf("NewSSHDriver password: %v", err)
	}
	if err := pwDrv.Test(ctx); err != nil {
		t.Fatalf("password SSH Test: %v", err)
	}

	// Verify local container SSH key file (explicit path and HOME auto-discovery).
	fakeHome := t.TempDir()
	sshDir := filepath.Join(fakeHome, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("mkdir .ssh: %v", err)
	}
	localKeyFile := filepath.Join(sshDir, "id_ed25519")
	if err := os.WriteFile(localKeyFile, []byte(privPEM), 0o600); err != nil {
		t.Fatalf("write local key file: %v", err)
	}

	localFileDrv, err := NewSSHDriver(models.StorageProvider{
		Type:           "ssh",
		Host:           host,
		Port:           port,
		Username:       "backupuser",
		SSHUseLocalKey: true,
		SSHKeyPath:     localKeyFile,
		RemotePath:     "homelab/vault",
	})
	if err != nil {
		t.Fatalf("NewSSHDriver local key path: %v", err)
	}
	if err := localFileDrv.Test(ctx); err != nil {
		t.Fatalf("local key path SSH Test: %v", err)
	}

	t.Setenv("HOME", fakeHome)
	autoDrv, err := NewSSHDriver(models.StorageProvider{
		Type:           "ssh",
		Host:           host,
		Port:           port,
		Username:       "backupuser",
		SSHUseLocalKey: true,
		RemotePath:     "homelab/vault",
	})
	if err != nil {
		t.Fatalf("NewSSHDriver auto-discover HOME/.ssh: %v", err)
	}
	if err := autoDrv.Test(ctx); err != nil {
		t.Fatalf("auto-discover local SSH Test: %v", err)
	}
}

func TestSMBAndNFSDriverValidation(t *testing.T) {
	if _, err := NewSMBDriver(models.StorageProvider{Type: "smb", Host: "nas.local"}); err == nil {
		t.Fatalf("expected error when smbShare missing")
	}
	smbDrv, err := NewSMBDriver(models.StorageProvider{
		Type:       "smb",
		Host:       "nas.local",
		SMBShare:   `\\nas.local\backups\`,
		RemotePath: `/dockvault/node2/`,
		Username:   "admin",
		Password:   "pass",
		SMBDomain:  "WORKGROUP",
	})
	if err != nil {
		t.Fatalf("NewSMBDriver valid: %v", err)
	}
	if smbDrv.shareName != "backups" || smbDrv.port != 445 || smbDrv.subDir != "dockvault/node2" {
		t.Fatalf("unexpected SMB normalization: %+v", smbDrv)
	}
	if got := toSMBPath(smbDrv.posixPath("app.tar.gz")); got != `dockvault\node2\app.tar.gz` {
		t.Fatalf("unexpected SMB path %q", got)
	}

	if _, err := NewSMBDriver(models.StorageProvider{
		Type:       "smb",
		Host:       "nas.local",
		SMBShare:   "backups",
		RemotePath: "../../etc",
	}); err == nil {
		t.Fatalf("expected traversal rejection in SMB RemotePath")
	}

	if _, err := NewNFSDriver(models.StorageProvider{Type: "nfs", Host: "nas.local"}); err == nil {
		t.Fatalf("expected error when nfsExport missing")
	}
	nfsDrv, err := NewNFSDriver(models.StorageProvider{
		Type:       "nfs",
		Host:       "192.168.1.20:2049",
		NFSExport:  "volume1/backups/",
		RemotePath: "/dockvault/",
		NFSUID:     1000,
		NFSGID:     1000,
	})
	if err != nil {
		t.Fatalf("NewNFSDriver valid: %v", err)
	}
	if nfsDrv.host != "192.168.1.20" || nfsDrv.exportPath != "/volume1/backups" || nfsDrv.subDir != "dockvault" {
		t.Fatalf("unexpected NFS normalization: %+v", nfsDrv)
	}
}
