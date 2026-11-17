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

func TestCompressionRoundTrip(t *testing.T) {
	payload := bytes.Repeat([]byte("dockvault-zstd-gzip-roundtrip-"), 1500)
	for _, algo := range []string{"gzip", "zstd"} {
		var buf bytes.Buffer
		cw, err := NewCompressedWriter(&buf, algo)
		if err != nil {
			t.Fatalf("%s writer: %v", algo, err)
		}
		if _, err := cw.Write(payload); err != nil {
			t.Fatalf("%s write: %v", algo, err)
		}
		if err := cw.Close(); err != nil {
			t.Fatalf("%s close: %v", algo, err)
		}

		dr, err := OpenCompressedReader(bytes.NewReader(buf.Bytes()), "")
		if err != nil {
			t.Fatalf("%s OpenCompressedReader: %v", algo, err)
		}
		got, err := io.ReadAll(dr)
		_ = dr.Close()
		if err != nil {
			t.Fatalf("%s read: %v", algo, err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("%s roundtrip mismatch: got %d, want %d", algo, len(got), len(payload))
		}
	}
}
