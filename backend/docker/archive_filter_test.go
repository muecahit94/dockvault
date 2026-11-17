package docker

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"
)

func buildTar(t *testing.T, entries []tar.Header, bodies map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, hdr := range entries {
		h := hdr
		body := bodies[h.Name]
		h.Size = int64(len(body))
		if h.Typeflag == tar.TypeDir {
			h.Size = 0
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if body != "" {
			if _, err := tw.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func readTarNames(t *testing.T, r io.Reader) (names []string, bodies map[string]string, err error) {
	t.Helper()
	tr := tar.NewReader(r)
	bodies = map[string]string{}
	for {
		hdr, nextErr := tr.Next()
		if errors.Is(nextErr, io.EOF) {
			return names, bodies, nil
		}
		if nextErr != nil {
			return names, bodies, nextErr
		}
		names = append(names, hdr.Name)
		raw, _ := io.ReadAll(tr)
		bodies[hdr.Name] = string(raw)
	}
}

func TestFilterTarStreamKeepsSelectedEntriesAndAncestors(t *testing.T) {
	archive := buildTar(t, []tar.Header{
		{Name: "./", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "./data/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "./data/conf/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "./data/conf/app.yml", Typeflag: tar.TypeReg, Mode: 0o644},
		{Name: "./data/conf/secret.yml", Typeflag: tar.TypeReg, Mode: 0o644},
		{Name: "./data/db/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "./data/db/main.sqlite", Typeflag: tar.TypeReg, Mode: 0o644},
		{Name: "./data/db/wal/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "./data/db/wal/0001", Typeflag: tar.TypeReg, Mode: 0o644},
		{Name: "./other/x.txt", Typeflag: tar.TypeReg, Mode: 0o644},
	}, map[string]string{
		"./data/conf/app.yml":    "app",
		"./data/conf/secret.yml": "secret",
		"./data/db/main.sqlite":  "db",
		"./data/db/wal/0001":     "wal",
		"./other/x.txt":          "x",
	})

	// "data/db/wal/0001" is redundant because its parent directory is also selected.
	selected := NormalizeArchivePaths([]string{"data/conf/app.yml", "./data/db/", "/data/db/wal/0001", ""})
	if want := []string{"data/conf/app.yml", "data/db"}; !reflect.DeepEqual(selected, want) {
		t.Fatalf("NormalizeArchivePaths = %v, want %v", selected, want)
	}

	filtered, stop := FilterTarStream(bytes.NewReader(archive), selected)
	defer stop()
	names, bodies, err := readTarNames(t, filtered)
	if err != nil {
		t.Fatalf("read filtered tar: %v", err)
	}
	want := []string{
		"./data/",
		"./data/conf/",
		"./data/conf/app.yml",
		"./data/db/",
		"./data/db/main.sqlite",
		"./data/db/wal/",
		"./data/db/wal/0001",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("filtered entries = %v, want %v", names, want)
	}
	if bodies["./data/conf/app.yml"] != "app" || bodies["./data/db/wal/0001"] != "wal" {
		t.Fatalf("file contents not preserved: %v", bodies)
	}
}

func TestFilterTarStreamErrorsWhenNothingMatches(t *testing.T) {
	archive := buildTar(t, []tar.Header{
		{Name: "./data/a.txt", Typeflag: tar.TypeReg, Mode: 0o644},
	}, map[string]string{"./data/a.txt": "a"})

	filtered, stop := FilterTarStream(bytes.NewReader(archive), []string{"data/missing.txt"})
	defer stop()
	_, err := io.ReadAll(filtered)
	if !errors.Is(err, ErrNoMatchingEntries) {
		t.Fatalf("expected ErrNoMatchingEntries, got %v", err)
	}
}

func TestFilterTarStreamStopUnblocksAbandonedReader(t *testing.T) {
	body := string(bytes.Repeat([]byte("x"), 256*1024))
	archive := buildTar(t, []tar.Header{
		{Name: "./data/big.bin", Typeflag: tar.TypeReg, Mode: 0o644},
	}, map[string]string{"./data/big.bin": body})

	filtered, stop := FilterTarStream(bytes.NewReader(archive), []string{"data"})
	// Read a little, then abandon the stream as a failed restore would.
	if _, err := filtered.Read(make([]byte, 512)); err != nil {
		t.Fatalf("initial read: %v", err)
	}
	finished := make(chan struct{})
	go func() {
		stop()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("stop() did not unblock the filtering goroutine")
	}
}
