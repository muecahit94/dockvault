package docker

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
)

const maxNoteLength = 500

// ErrNoMatchingEntries is returned when a selective restore matches nothing in the archive.
var ErrNoMatchingEntries = errors.New("none of the selected paths exist in the archive")

// ArchivePathKey normalizes tar entry names ("./data/x", "data/x/") to a comparable key ("data/x").
func ArchivePathKey(name string) string {
	cleaned := path.Clean("/" + strings.TrimSpace(name))
	return strings.TrimPrefix(cleaned, "/")
}

// NormalizeArchivePaths cleans, de-duplicates, and drops paths already covered by a selected parent.
func NormalizeArchivePaths(paths []string) []string {
	set := make(map[string]bool, len(paths))
	for _, p := range paths {
		if key := ArchivePathKey(p); key != "" {
			set[key] = true
		}
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		covered := false
		for _, parent := range out {
			if strings.HasPrefix(k, parent+"/") {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, k)
		}
	}
	return out
}

// FilterTarStream returns a tar stream containing only entries at or below the selected paths,
// plus their ancestor directory entries so extraction recreates the directory structure.
// Callers must call the returned stop function (even after a full read) to unblock and wait
// for the filtering goroutine before closing src.
func FilterTarStream(src io.Reader, selected []string) (io.Reader, func()) {
	pr, pw := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		pw.CloseWithError(filterTar(src, pw, selected))
	}()
	stop := func() {
		_ = pr.Close()
		<-done
	}
	return pr, stop
}

func filterTar(src io.Reader, dst io.Writer, selected []string) error {
	tr := tar.NewReader(src)
	tw := tar.NewWriter(dst)
	matched := 0
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read archive entries: %w", err)
		}
		key := ArchivePathKey(hdr.Name)
		if key == "" {
			continue
		}
		selectedMatch, ancestor := matchSelection(key, selected)
		if !selectedMatch && !(ancestor && hdr.Typeflag == tar.TypeDir) {
			continue
		}
		if selectedMatch {
			matched++
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("write filtered entry %s: %w", hdr.Name, err)
		}
		if _, err := io.Copy(tw, tr); err != nil {
			return fmt.Errorf("copy filtered entry %s: %w", hdr.Name, err)
		}
	}
	if matched == 0 {
		return ErrNoMatchingEntries
	}
	return tw.Close()
}

// matchSelection reports whether key is a selected path (or inside one), or an ancestor of one.
func matchSelection(key string, selected []string) (match bool, ancestor bool) {
	for _, p := range selected {
		if key == p || strings.HasPrefix(key, p+"/") {
			return true, false
		}
		if strings.HasPrefix(p, key+"/") {
			ancestor = true
		}
	}
	return false, ancestor
}

func truncateNote(note string) string {
	note = strings.TrimSpace(note)
	if r := []rune(note); len(r) > maxNoteLength {
		return string(r[:maxNoteLength])
	}
	return note
}
