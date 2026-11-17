package docker

import "testing"

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
