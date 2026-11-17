package docker

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestToMountInfoFiltersDockerSocket(t *testing.T) {
	sock := toMountInfo("bind", "", "/var/run/docker.sock", "/var/run/docker.sock", "", true)
	if !sock.IsDockerSocket || sock.Backupable {
		t.Fatalf("expected docker.sock to be flagged and excluded from backups: %+v", sock)
	}

	vol := toMountInfo("volume", "pg_data", "/var/lib/docker/volumes/pg_data/_data", "/var/lib/postgresql/data", "", true)
	if vol.IsDockerSocket || !vol.Backupable {
		t.Fatalf("expected named volume to be backupable: %+v", vol)
	}

	tmp := toMountInfo("tmpfs", "", "", "/tmp", "", true)
	if tmp.Backupable {
		t.Fatalf("expected tmpfs mount not to be backupable: %+v", tmp)
	}
}

func TestSanitizeSlug(t *testing.T) {
	cases := map[string]string{
		"my-postgres_1": "my-postgres_1",
		"/web/app":      "web_app",
		"   ":           "backup",
		"db@prod!":      "db_prod",
	}
	for input, want := range cases {
		if got := sanitizeSlug(input); got != want {
			t.Fatalf("sanitizeSlug(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestDetectDBEngineAndExcludes(t *testing.T) {
	if got := DetectDBEngine("postgres:16-alpine"); got != "postgres" {
		t.Fatalf("expected postgres, got %q", got)
	}
	if got := DetectDBEngine("ghcr.io/immich-app/postgres:14-vectorchord"); got != "postgres" {
		t.Fatalf("expected postgres with registry path, got %q", got)
	}
	if got := DetectDBEngine("mariadb:11"); got != "mariadb" {
		t.Fatalf("expected mariadb, got %q", got)
	}
	if got := DetectDBEngine("redis:7-alpine"); got != "redis" {
		t.Fatalf("expected redis, got %q", got)
	}
	if got := DetectDBEngine("nginx:alpine"); got != "" {
		t.Fatalf("expected empty for nginx, got %q", got)
	}

	gotExcludes := ParseExcludePatterns("# ignore comment\n/tmp/*, *.log; **/cache/**\n*.log\n")
	if len(gotExcludes) != 3 || gotExcludes[0] != "tmp/*" || gotExcludes[1] != "*.log" || gotExcludes[2] != "**/cache/**" {
		t.Fatalf("unexpected excludes: %+v", gotExcludes)
	}
}

func TestProgressHubLifecycle(t *testing.T) {
	hub := NewProgressHub()
	ch, unsub := hub.Subscribe()
	defer unsub()

	tr := hub.StartJob(42, "pg-stack", "stack")
	tr.BeginStep("archive", "Archive & Compress", "ZSTD · 2 mounts")
	tr.UpdateBytes(2048)
	stepsJSON := tr.Finish("success", "")

	var steps []JobStep
	if err := json.Unmarshal([]byte(stepsJSON), &steps); err != nil || len(steps) != 1 || steps[0].Status != "done" {
		t.Fatalf("unexpected steps JSON: %s (%v)", stepsJSON, err)
	}
	if got, ok := hub.Get(42); !ok || got.Status != "success" || got.BytesProcessed != 2048 {
		t.Fatalf("unexpected hub snapshot: ok=%v got=%+v", ok, got)
	}

	select {
	case snap := <-ch:
		if len(snap) == 0 {
			t.Fatalf("expected subscriber snapshot")
		}
	default:
		t.Fatalf("expected buffered update on subscriber channel")
	}
}

func TestCopyTarAndAppendDB(t *testing.T) {
	var srcTar bytes.Buffer
	tw := tar.NewWriter(&srcTar)
	payload := []byte("hello volume")
	_ = tw.WriteHeader(&tar.Header{Name: "vol_data/file.txt", Size: int64(len(payload)), Mode: 0o644, Typeflag: tar.TypeReg})
	_, _ = tw.Write(payload)
	_ = tw.Close()

	dumpFile := filepath.Join(t.TempDir(), "db.sql")
	if err := os.WriteFile(dumpFile, []byte("CREATE TABLE t(id int);"), 0o600); err != nil {
		t.Fatalf("write dump: %v", err)
	}

	var outTar bytes.Buffer
	err := copyTarAndAppendDB(&srcTar, &outTar, []dbDumpSpec{{
		ArchivePath: "db_dumps/pg-postgres.sql",
		TempPath:    dumpFile,
		Size:        23,
	}})
	if err != nil {
		t.Fatalf("copyTarAndAppendDB: %v", err)
	}

	tr := tar.NewReader(&outTar)
	var seen []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read combined tar: %v", err)
		}
		seen = append(seen, hdr.Name)
	}
	if len(seen) != 3 || seen[0] != "vol_data/file.txt" || seen[2] != "db_dumps/pg-postgres.sql" {
		t.Fatalf("unexpected tar entries: %+v", seen)
	}
}
