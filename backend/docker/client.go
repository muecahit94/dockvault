package docker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"gorm.io/gorm"
)

// VolumeInfo describes a Docker named volume and whether any container uses it.
type VolumeInfo struct {
	Name               string   `json:"name"`
	Driver             string   `json:"driver"`
	Mountpoint         string   `json:"mountpoint"`
	Scope              string   `json:"scope"`
	CreatedAt          string   `json:"createdAt"`
	AttachedContainers []string `json:"attachedContainers"`
	InUse              bool     `json:"inUse"`
}

// MountInfo describes a container mount discovered from the Docker Engine.
type MountInfo struct {
	Type           string `json:"type"`
	Name           string `json:"name"`
	Source         string `json:"source"`
	Destination    string `json:"destination"`
	Mode           string `json:"mode"`
	RW             bool   `json:"rw"`
	IsDockerSocket bool   `json:"isDockerSocket"`
	Backupable     bool   `json:"backupable"`
}

// ContainerInfo represents a discovered Docker container and its backup-relevant metadata.
type ContainerInfo struct {
	ID                 string      `json:"id"`
	ShortID            string      `json:"shortId"`
	Name               string      `json:"name"`
	Image              string      `json:"image"`
	State              string      `json:"state"`
	Status             string      `json:"status"`
	Created            int64       `json:"created"`
	IsSelf             bool        `json:"isSelf"`
	Excluded           bool        `json:"excluded"`
	PreHook            string      `json:"preHook,omitempty"`
	PostHook           string      `json:"postHook,omitempty"`
	ComposeProject     string      `json:"composeProject"`
	ComposeService     string      `json:"composeService"`
	ComposeConfigFiles string      `json:"composeConfigFiles"`
	ComposeWorkingDir  string      `json:"composeWorkingDir"`
	Mounts             []MountInfo `json:"mounts"`
	BackupableCount    int         `json:"backupableCount"`
	LastBackupAt       *time.Time  `json:"lastBackupAt,omitempty"`
	LastBackupStatus   string      `json:"lastBackupStatus,omitempty"`
}

// ComposeStackInfo groups containers belonging to the same Docker Compose project.
type ComposeStackInfo struct {
	Name             string          `json:"name"`
	WorkingDir       string          `json:"workingDir"`
	ConfigFiles      string          `json:"configFiles"`
	Containers       []ContainerInfo `json:"containers"`
	RunningCount     int             `json:"runningCount"`
	TotalCount       int             `json:"totalCount"`
	UniqueMounts     []MountInfo     `json:"uniqueMounts"`
	BackupableCount  int             `json:"backupableCount"`
	LastBackupAt     *time.Time      `json:"lastBackupAt,omitempty"`
	LastBackupStatus string          `json:"lastBackupStatus,omitempty"`
}

// DaemonStatus summarizes connectivity to the local Docker socket.
type DaemonStatus struct {
	Connected     bool   `json:"connected"`
	ServerVersion string `json:"serverVersion,omitempty"`
	OS            string `json:"os,omitempty"`
	Architecture  string `json:"architecture,omitempty"`
	Name          string `json:"name,omitempty"`
	Error         string `json:"error,omitempty"`
}

// Service manages Docker discovery and volume backups.
type Service struct {
	cli                  *client.Client
	db                   *gorm.DB
	backupDir            string
	helperImage          string
	defaultEncryptionKey string
	selfHostname         string
	mu                   sync.Mutex
	activeTargets        map[string]bool
}

// NewService creates a Docker Service connected via environment/default socket.
func NewService(db *gorm.DB, backupDir, helperImage string, defaultEncryptionKey ...string) (*Service, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}

	encKey := ""
	if len(defaultEncryptionKey) > 0 {
		encKey = defaultEncryptionKey[0]
	}

	hostname, _ := os.Hostname()
	return &Service{
		cli:                  cli,
		db:                   db,
		backupDir:            backupDir,
		helperImage:          helperImage,
		defaultEncryptionKey: encKey,
		selfHostname:         strings.TrimSpace(hostname),
		activeTargets:        make(map[string]bool),
	}, nil
}

// Close releases the underlying Docker HTTP client.
func (s *Service) Close() error {
	if s.cli == nil {
		return nil
	}
	return s.cli.Close()
}

// Status verifies Docker daemon connectivity and returns engine metadata.
func (s *Service) Status(ctx context.Context) DaemonStatus {
	info, err := s.cli.Info(ctx)
	if err != nil {
		return DaemonStatus{
			Connected: false,
			Error:     err.Error(),
		}
	}
	return DaemonStatus{
		Connected:     true,
		ServerVersion: info.ServerVersion,
		OS:            info.OperatingSystem,
		Architecture:  info.Architecture,
		Name:          info.Name,
	}
}

// ListContainers discovers Docker containers and their attached mounts.
func (s *Service) ListContainers(ctx context.Context, includeAll bool) ([]ContainerInfo, error) {
	raw, err := s.cli.ContainerList(ctx, container.ListOptions{All: includeAll})
	if err != nil {
		return nil, fmt.Errorf("list docker containers: %w", err)
	}

	result := make([]ContainerInfo, 0, len(raw))
	for _, c := range raw {
		name := c.ID[:min(12, len(c.ID))]
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}

		mounts := make([]MountInfo, 0, len(c.Mounts))
		backupableCount := 0
		for _, m := range c.Mounts {
			mi := toMountInfo(string(m.Type), m.Name, m.Source, m.Destination, m.Mode, m.RW)
			if mi.Backupable {
				backupableCount++
			}
			mounts = append(mounts, mi)
		}

		shortID := c.ID
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}

		isSelf := s.selfHostname != "" && (s.selfHostname == shortID || s.selfHostname == c.ID || s.selfHostname == name)
		excluded := strings.EqualFold(c.Labels["dockvault.exclude"], "true") ||
			strings.EqualFold(c.Labels["dockvault.enable"], "false")

		result = append(result, ContainerInfo{
			ID:                 c.ID,
			ShortID:            shortID,
			Name:               name,
			Image:              c.Image,
			State:              c.State,
			Status:             c.Status,
			Created:            c.Created,
			IsSelf:             isSelf,
			Excluded:           excluded,
			PreHook:            strings.TrimSpace(c.Labels["dockvault.pre-backup"]),
			PostHook:           strings.TrimSpace(c.Labels["dockvault.post-backup"]),
			ComposeProject:     c.Labels["com.docker.compose.project"],
			ComposeService:     c.Labels["com.docker.compose.service"],
			ComposeConfigFiles: c.Labels["com.docker.compose.project.config_files"],
			ComposeWorkingDir:  c.Labels["com.docker.compose.project.working_dir"],
			Mounts:             mounts,
			BackupableCount:    backupableCount,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}

// ListStacks groups discovered containers by their Docker Compose project label.
func (s *Service) ListStacks(ctx context.Context, includeAll bool) ([]ComposeStackInfo, error) {
	containers, err := s.ListContainers(ctx, includeAll)
	if err != nil {
		return nil, err
	}

	byProject := make(map[string]*ComposeStackInfo)
	seenMounts := make(map[string]map[string]bool)

	for _, c := range containers {
		if c.ComposeProject == "" {
			continue
		}
		stack, ok := byProject[c.ComposeProject]
		if !ok {
			stack = &ComposeStackInfo{
				Name:        c.ComposeProject,
				WorkingDir:  c.ComposeWorkingDir,
				ConfigFiles: c.ComposeConfigFiles,
			}
			byProject[c.ComposeProject] = stack
			seenMounts[c.ComposeProject] = make(map[string]bool)
		}

		stack.Containers = append(stack.Containers, c)
		stack.TotalCount++
		if c.State == "running" {
			stack.RunningCount++
		}

		for _, m := range c.Mounts {
			if !m.Backupable {
				continue
			}
			key := mountDedupKey(m)
			if !seenMounts[c.ComposeProject][key] {
				seenMounts[c.ComposeProject][key] = true
				stack.UniqueMounts = append(stack.UniqueMounts, m)
				stack.BackupableCount++
			}
		}
	}

	stacks := make([]ComposeStackInfo, 0, len(byProject))
	for _, stack := range byProject {
		stacks = append(stacks, *stack)
	}
	sort.Slice(stacks, func(i, j int) bool {
		return stacks[i].Name < stacks[j].Name
	})
	return stacks, nil
}

// ListVolumes lists all Docker named volumes and cross-references container attachments.
func (s *Service) ListVolumes(ctx context.Context) ([]VolumeInfo, error) {
	volResp, err := s.cli.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list docker volumes: %w", err)
	}

	attached := make(map[string][]string)
	if containers, err := s.ListContainers(ctx, true); err == nil {
		for _, c := range containers {
			for _, m := range c.Mounts {
				if m.Type == "volume" && m.Name != "" {
					attached[m.Name] = append(attached[m.Name], c.Name)
				}
			}
		}
	}

	out := make([]VolumeInfo, 0, len(volResp.Volumes))
	for _, v := range volResp.Volumes {
		ctrs := attached[v.Name]
		sort.Strings(ctrs)
		out = append(out, VolumeInfo{
			Name:               v.Name,
			Driver:             v.Driver,
			Mountpoint:         v.Mountpoint,
			Scope:              v.Scope,
			CreatedAt:          v.CreatedAt,
			AttachedContainers: ctrs,
			InUse:              len(ctrs) > 0,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].InUse != out[j].InUse {
			return !out[i].InUse // Show orphan volumes first so they're easy to spot.
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// RemoveVolume deletes an unattached Docker volume.
func (s *Service) RemoveVolume(ctx context.Context, name string) error {
	vols, err := s.ListVolumes(ctx)
	if err != nil {
		return err
	}
	for _, v := range vols {
		if v.Name == name && v.InUse {
			return fmt.Errorf("cannot delete volume %q: currently attached to %s", name, strings.Join(v.AttachedContainers, ", "))
		}
	}
	if err := s.cli.VolumeRemove(ctx, name, false); err != nil {
		return fmt.Errorf("remove volume %q: %w", name, err)
	}
	return nil
}

func toMountInfo(mountType, name, source, destination, mode string, rw bool) MountInfo {
	cleanSrc := filepath.Clean(source)
	cleanDst := filepath.Clean(destination)
	isSock := strings.HasSuffix(cleanSrc, "docker.sock") || strings.HasSuffix(cleanDst, "docker.sock")
	backupable := (mountType == "volume" || mountType == "bind") && !isSock && cleanSrc != ""

	return MountInfo{
		Type:           mountType,
		Name:           name,
		Source:         source,
		Destination:    destination,
		Mode:           mode,
		RW:             rw,
		IsDockerSocket: isSock,
		Backupable:     backupable,
	}
}

func mountDedupKey(m MountInfo) string {
	if m.Type == "volume" && m.Name != "" {
		return "volume:" + m.Name
	}
	return m.Type + ":" + filepath.Clean(m.Source)
}
