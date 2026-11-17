package crypto

import (
	"bufio"
	"compress/gzip"
	"io"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// NormalizeCompression returns either "zstd" or "gzip" (default).
func NormalizeCompression(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "zstd", "zst":
		return "zstd"
	default:
		return "gzip"
	}
}

// ArchiveExtension returns the archive filename suffix for the given compression algorithm.
func ArchiveExtension(compression string) string {
	if NormalizeCompression(compression) == "zstd" {
		return ".tar.zst"
	}
	return ".tar.gz"
}

// IsZstdFile checks if the name ends with .zst or .zst.enc.
func IsZstdFile(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	return strings.HasSuffix(lower, ".tar.zst") || strings.HasSuffix(lower, ".tar.zst.enc")
}

// NewCompressedWriter wraps w with gzip or zstd compression.
func NewCompressedWriter(w io.Writer, compression string) (io.WriteCloser, error) {
	if NormalizeCompression(compression) == "zstd" {
		return zstd.NewWriter(w, zstd.WithEncoderLevel(zstd.SpeedDefault))
	}
	return gzip.NewWriter(w), nil
}

type zstdReadCloser struct {
	*zstd.Decoder
}

func (z zstdReadCloser) Close() error {
	z.Decoder.Close()
	return nil
}

type gzipReadCloser struct {
	*gzip.Reader
	underlying io.Closer
}

func (g gzipReadCloser) Close() error {
	err := g.Reader.Close()
	if g.underlying != nil {
		_ = g.underlying.Close()
	}
	return err
}

type zstdClosingReader struct {
	*zstd.Decoder
	underlying io.Closer
}

func (z zstdClosingReader) Close() error {
	z.Decoder.Close()
	if z.underlying != nil {
		return z.underlying.Close()
	}
	return nil
}

// OpenCompressedReader auto-detects gzip vs zstd (by magic header bytes, falling back to filenameHint)
// and returns a ReadCloser streaming the decompressed tar payload.
func OpenCompressedReader(r io.Reader, filenameHint string) (io.ReadCloser, error) {
	br := bufio.NewReaderSize(r, 16*1024)
	magic, _ := br.Peek(4)

	var closer io.Closer
	if c, ok := r.(io.Closer); ok {
		closer = c
	}

	isZstd := len(magic) >= 4 && magic[0] == 0x28 && magic[1] == 0xb5 && magic[2] == 0x2f && magic[3] == 0xfd
	isGzip := len(magic) >= 2 && magic[0] == 0x1f && magic[1] == 0x8b
	if !isZstd && !isGzip && IsZstdFile(filenameHint) {
		isZstd = true
	}

	if isZstd {
		dec, err := zstd.NewReader(br)
		if err != nil {
			if closer != nil {
				_ = closer.Close()
			}
			return nil, err
		}
		if closer != nil {
			return zstdClosingReader{Decoder: dec, underlying: closer}, nil
		}
		return zstdReadCloser{Decoder: dec}, nil
	}

	gz, err := gzip.NewReader(br)
	if err != nil {
		if closer != nil {
			_ = closer.Close()
		}
		return nil, err
	}
	return gzipReadCloser{Reader: gz, underlying: closer}, nil
}
