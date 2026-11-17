package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/scrypt"
)

const (
	magicHeader = "DVENC1\x00\x01"
	saltSize    = 16
	nonceSize   = 12
	chunkSize   = 64 * 1024 // 64 KiB streaming chunks
)

// ErrInvalidArchive indicates the stream is not a valid DockVault encrypted archive or passphrase is wrong.
var ErrInvalidArchive = errors.New("decryption failed: invalid passphrase or corrupted encrypted archive")

func deriveKey(passphrase string, salt []byte) ([]byte, error) {
	// scrypt with N=16384 (2^14), r=8, p=1 gives strong KDF without stalling low-RAM containers.
	return scrypt.Key([]byte(passphrase), salt, 16384, 8, 1, 32)
}

// EncryptFile encrypts srcPath into a new temporary file using chunked AES-256-GCM
// and returns the encrypted file path, byte size, and SHA-256 hex digest of the encrypted file.
func EncryptFile(srcPath, destPath, passphrase string) (int64, string, error) {
	if passphrase == "" {
		return 0, "", fmt.Errorf("encryption passphrase cannot be empty")
	}

	in, err := os.Open(srcPath)
	if err != nil {
		return 0, "", fmt.Errorf("open plaintext archive: %w", err)
	}
	defer in.Close()

	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return 0, "", fmt.Errorf("create encrypted archive: %w", err)
	}
	defer out.Close()

	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return 0, "", fmt.Errorf("generate salt: %w", err)
	}

	key, err := deriveKey(passphrase, salt)
	if err != nil {
		return 0, "", fmt.Errorf("derive encryption key: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return 0, "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return 0, "", err
	}

	hasher := sha256.New()
	mw := io.MultiWriter(out, hasher)
	var totalBytes int64

	writeChecked := func(data []byte) error {
		n, wErr := mw.Write(data)
		totalBytes += int64(n)
		return wErr
	}

	if err := writeChecked([]byte(magicHeader)); err != nil {
		return 0, "", err
	}
	if err := writeChecked(salt); err != nil {
		return 0, "", err
	}

	buf := make([]byte, chunkSize)
	nonce := make([]byte, nonceSize)
	var lenBuf [4]byte

	for {
		n, readErr := in.Read(buf)
		if n > 0 {
			if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
				return 0, "", err
			}
			ciphertext := gcm.Seal(nil, nonce, buf[:n], nil)
			binary.BigEndian.PutUint32(lenBuf[:], uint32(len(ciphertext)))
			if err := writeChecked(lenBuf[:]); err != nil {
				return 0, "", err
			}
			if err := writeChecked(nonce); err != nil {
				return 0, "", err
			}
			if err := writeChecked(ciphertext); err != nil {
				return 0, "", err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return 0, "", fmt.Errorf("read plaintext chunk: %w", readErr)
		}
	}

	// Zero-length EOF marker prevents silent truncation attacks.
	binary.BigEndian.PutUint32(lenBuf[:], 0)
	if err := writeChecked(lenBuf[:]); err != nil {
		return 0, "", err
	}

	return totalBytes, fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

// DecryptReader wraps an encrypted archive stream and yields decrypted plaintext bytes on the fly.
func DecryptReader(src io.ReadCloser, passphrase string) (io.ReadCloser, error) {
	if passphrase == "" {
		_ = src.Close()
		return nil, fmt.Errorf("encryption passphrase is required to decrypt this archive")
	}

	header := make([]byte, len(magicHeader)+saltSize)
	if _, err := io.ReadFull(src, header); err != nil {
		_ = src.Close()
		return nil, fmt.Errorf("%w: %v", ErrInvalidArchive, err)
	}
	if string(header[:len(magicHeader)]) != magicHeader {
		_ = src.Close()
		return nil, fmt.Errorf("%w: unrecognized header", ErrInvalidArchive)
	}

	salt := header[len(magicHeader):]
	key, err := deriveKey(passphrase, salt)
	if err != nil {
		_ = src.Close()
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		_ = src.Close()
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		_ = src.Close()
		return nil, err
	}

	return &decryptReadCloser{
		src: src,
		gcm: gcm,
	}, nil
}

type decryptReadCloser struct {
	src io.ReadCloser
	gcm cipher.AEAD
	buf []byte
	eof bool
}

func (d *decryptReadCloser) Read(p []byte) (int, error) {
	for len(d.buf) == 0 {
		if d.eof {
			return 0, io.EOF
		}
		var lenBuf [4]byte
		if _, err := io.ReadFull(d.src, lenBuf[:]); err != nil {
			return 0, fmt.Errorf("%w: unexpected EOF", ErrInvalidArchive)
		}
		cipherLen := binary.BigEndian.Uint32(lenBuf[:])
		if cipherLen == 0 {
			d.eof = true
			return 0, io.EOF
		}
		if cipherLen > chunkSize+1024 {
			return 0, fmt.Errorf("%w: invalid chunk size", ErrInvalidArchive)
		}

		nonce := make([]byte, nonceSize)
		if _, err := io.ReadFull(d.src, nonce); err != nil {
			return 0, fmt.Errorf("%w: truncated nonce", ErrInvalidArchive)
		}
		ciphertext := make([]byte, cipherLen)
		if _, err := io.ReadFull(d.src, ciphertext); err != nil {
			return 0, fmt.Errorf("%w: truncated ciphertext", ErrInvalidArchive)
		}

		plain, err := d.gcm.Open(nil, nonce, ciphertext, nil)
		if err != nil {
			return 0, ErrInvalidArchive
		}
		d.buf = plain
	}

	n := copy(p, d.buf)
	d.buf = d.buf[n:]
	return n, nil
}

func (d *decryptReadCloser) Close() error {
	return d.src.Close()
}
