package crypto

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptAndDecryptRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "plain.tar.gz")
	enc := filepath.Join(dir, "plain.tar.gz.enc")

	// Larger than 64 KiB chunk size to verify multi-chunk streaming.
	original := bytes.Repeat([]byte("dockvault-encrypted-archive-payload-"), 4000)
	if err := os.WriteFile(src, original, 0o600); err != nil {
		t.Fatalf("write src: %v", err)
	}

	size, sum, err := EncryptFile(src, enc, "super-secret-passphrase")
	if err != nil {
		t.Fatalf("EncryptFile: %v", err)
	}
	if size <= int64(len(original)) || len(sum) != 64 {
		t.Fatalf("unexpected encrypted size/sum: size=%d sum=%q", size, sum)
	}

	f, err := os.Open(enc)
	if err != nil {
		t.Fatalf("open encrypted: %v", err)
	}
	dec, err := DecryptReader(f, "super-secret-passphrase")
	if err != nil {
		t.Fatalf("DecryptReader: %v", err)
	}
	got, err := io.ReadAll(dec)
	_ = dec.Close()
	if err != nil {
		t.Fatalf("read decrypted: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("decrypted content mismatch: got %d bytes, want %d", len(got), len(original))
	}

	// Wrong passphrase must fail authentication.
	f2, _ := os.Open(enc)
	dec2, err := DecryptReader(f2, "wrong-passphrase")
	if err == nil {
		_, readErr := io.ReadAll(dec2)
		_ = dec2.Close()
		if readErr == nil {
			t.Fatalf("expected wrong passphrase to fail decryption")
		}
	}
}
