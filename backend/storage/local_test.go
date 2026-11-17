package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestSafeFileName(t *testing.T) {
	valid, err := SafeFileName("postgres-20261117.tar.gz")
	if err != nil || valid != "postgres-20261117.tar.gz" {
		t.Fatalf("expected valid filename, got %q, err=%v", valid, err)
	}

	// Base strips directory traversal prefixes, leaving only the safe basename.
	stripped, err := SafeFileName("../etc/passwd")
	if err != nil || stripped != "passwd" {
		t.Fatalf("expected stripped basename 'passwd', got %q, err=%v", stripped, err)
	}

	for _, bad := range []string{"", ".", "..", "/"} {
		if _, err := SafeFileName(bad); err == nil {
			t.Fatalf("expected error for invalid filename %q", bad)
		}
	}
}

func TestLocalDriverLifecycleAndContainment(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	driver, err := NewLocalDriver(dir)
	if err != nil {
		t.Fatalf("NewLocalDriver failed: %v", err)
	}
	if err := driver.Test(ctx); err != nil {
		t.Fatalf("LocalDriver.Test failed: %v", err)
	}

	tmpSrc := filepath.Join(t.TempDir(), "archive.tar.gz")
	payload := []byte("mock-gzip-payload")
	if err := os.WriteFile(tmpSrc, payload, 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	savedPath, err := driver.Save(ctx, tmpSrc, "app-backup.tar.gz")
	if err != nil {
		t.Fatalf("LocalDriver.Save failed: %v", err)
	}

	rc, size, err := driver.Open(ctx, savedPath)
	if err != nil {
		t.Fatalf("LocalDriver.Open failed: %v", err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()

	if size != int64(len(payload)) || string(got) != string(payload) {
		t.Fatalf("unexpected payload: size=%d content=%q", size, string(got))
	}

	// Ensure reading outside the storage base directory is blocked.
	if _, _, err := driver.Open(ctx, filepath.Join(dir, "..", "outside.tar.gz")); err == nil {
		t.Fatalf("expected path containment error for outside path")
	}

	listed, err := driver.List(ctx)
	if err != nil || len(listed) != 1 || listed[0].Name != "app-backup.tar.gz" {
		t.Fatalf("expected 1 listed archive, got %+v err=%v", listed, err)
	}

	if err := driver.Delete(ctx, savedPath); err != nil {
		t.Fatalf("LocalDriver.Delete failed: %v", err)
	}
	if _, err := os.Stat(savedPath); !os.IsNotExist(err) {
		t.Fatalf("expected file to be deleted, stat err=%v", err)
	}
}
