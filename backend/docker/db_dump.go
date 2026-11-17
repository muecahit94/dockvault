package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

type dbDumpSpec struct {
	ArchivePath string
	TempPath    string
	Size        int64
}

// DetectDBEngine infers the database engine from a container image string.
func DetectDBEngine(image string) string {
	lower := strings.ToLower(strings.TrimSpace(image))
	if lower == "" {
		return ""
	}
	// Drop tag and digest, inspect final repository segment.
	if idx := strings.Index(lower, "@"); idx >= 0 {
		lower = lower[:idx]
	}
	if idx := strings.LastIndex(lower, ":"); idx >= 0 {
		slash := strings.LastIndex(lower, "/")
		if idx > slash {
			lower = lower[:idx]
		}
	}
	repo := lower
	if slash := strings.LastIndex(lower, "/"); slash >= 0 {
		repo = lower[slash+1:]
	}

	switch {
	case strings.Contains(repo, "postgres"), strings.Contains(repo, "postgis"), strings.Contains(repo, "timescale"):
		return "postgres"
	case strings.Contains(repo, "mariadb"):
		return "mariadb"
	case strings.Contains(repo, "mysql"), strings.Contains(repo, "percona"):
		return "mysql"
	case strings.Contains(repo, "mongo"):
		return "mongodb"
	case strings.Contains(repo, "redis"), strings.Contains(repo, "valkey"):
		return "redis"
	default:
		return ""
	}
}

func dbHotDumpCommand(engine string) (cmd string, ext string, ok bool) {
	switch engine {
	case "postgres":
		return `if [ -n "$PGPASSWORD" ]; then pg_dumpall -U "${POSTGRES_USER:-postgres}"; else PGPASSWORD="${POSTGRES_PASSWORD:-}" pg_dumpall -U "${POSTGRES_USER:-postgres}"; fi`, ".sql", true
	case "mariadb", "mysql":
		return `dump_bin=mysqldump; command -v mariadb-dump >/dev/null 2>&1 && dump_bin=mariadb-dump; PW="${MYSQL_ROOT_PASSWORD:-${MARIADB_ROOT_PASSWORD:-}}"; if [ -n "$PW" ]; then MYSQL_PWD="$PW" "$dump_bin" --all-databases --single-transaction -uroot; else "$dump_bin" --all-databases --single-transaction -uroot; fi`, ".sql", true
	case "mongodb":
		return `mongodump --archive`, ".archive", true
	case "redis":
		return `cmd=redis-cli; command -v valkey-cli >/dev/null 2>&1 && cmd=valkey-cli; if [ -n "$REDIS_PASSWORD" ]; then REDISCLI_AUTH="$REDIS_PASSWORD" "$cmd" --rdb -; else "$cmd" --rdb -; fi`, ".rdb", true
	default:
		return "", "", false
	}
}

func (s *Service) captureDBHotDump(ctx context.Context, c ContainerInfo) (*dbDumpSpec, error) {
	shellCmd, ext, ok := dbHotDumpCommand(c.DetectedDB)
	if !ok {
		return nil, nil
	}

	execResp, err := s.cli.ContainerExecCreate(ctx, c.ID, container.ExecOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          []string{"sh", "-c", shellCmd},
	})
	if err != nil {
		return nil, fmt.Errorf("create db dump exec on %s: %w", c.Name, err)
	}
	attachResp, err := s.cli.ContainerExecAttach(ctx, execResp.ID, container.ExecStartOptions{})
	if err != nil {
		return nil, fmt.Errorf("attach db dump exec on %s: %w", c.Name, err)
	}
	defer attachResp.Close()

	tmpFile, err := os.CreateTemp(s.backupDir, "dockvault-dbdump-*"+ext)
	if err != nil {
		return nil, fmt.Errorf("create temp db dump file: %w", err)
	}
	tmpPath := tmpFile.Name()

	var stderrBuf bytes.Buffer
	_, copyErr := stdcopy.StdCopy(tmpFile, &stderrBuf, attachResp.Reader)
	// stdcopy.StdCopy returns stdout+stderr combined; stat tmpFile for exact stdout dump size.
	info, statErr := tmpFile.Stat()
	closeErr := tmpFile.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("read db dump stdout on %s: %w", c.Name, copyErr)
	}
	if statErr != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("stat db dump temp file: %w", statErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("close db dump temp file: %w", closeErr)
	}

	inspect, err := s.cli.ContainerExecInspect(ctx, execResp.ID)
	if err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("inspect db dump exec on %s: %w", c.Name, err)
	}
	if inspect.ExitCode != 0 {
		_ = os.Remove(tmpPath)
		msg := strings.TrimSpace(stderrBuf.String())
		if msg == "" {
			msg = fmt.Sprintf("exit code %d", inspect.ExitCode)
		}
		return nil, fmt.Errorf("%s hot dump on %s failed: %s", c.DetectedDB, c.Name, msg)
	}
	dumpSize := info.Size()
	if dumpSize == 0 {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("%s hot dump on %s produced 0 bytes", c.DetectedDB, c.Name)
	}

	return &dbDumpSpec{
		ArchivePath: fmt.Sprintf("db_dumps/%s-%s%s", sanitizeSlug(c.Name), c.DetectedDB, ext),
		TempPath:    tmpPath,
		Size:        dumpSize,
	}, nil
}

// ParseExcludePatterns splits comma/newline/semicolon separated glob exclude patterns.
func ParseExcludePatterns(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	replacer := strings.NewReplacer("\r\n", "\n", ",", "\n", ";", "\n")
	lines := strings.Split(replacer.Replace(raw), "\n")
	out := make([]string, 0, len(lines))
	seen := make(map[string]bool)
	for _, line := range lines {
		p := strings.TrimSpace(line)
		if p == "" || strings.HasPrefix(p, "#") {
			continue
		}
		p = strings.TrimPrefix(p, "/")
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func normalizeExcludeList(patterns []string) []string {
	if len(patterns) == 0 {
		return nil
	}
	return ParseExcludePatterns(strings.Join(patterns, "\n"))
}

type progressCountingWriter struct {
	dst     io.Writer
	total   int64
	tracker *JobTracker
}

func (w *progressCountingWriter) Write(p []byte) (int, error) {
	n, err := w.dst.Write(p)
	if n > 0 {
		w.total += int64(n)
		if w.tracker != nil {
			w.tracker.UpdateBytes(w.total)
		}
	}
	return n, err
}
