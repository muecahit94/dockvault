package handlers

import (
	"github.com/gofiber/fiber/v2"
)

// ListVolumes returns Docker named volumes with attachment state and last backup metadata.
func (s *Server) ListVolumes(c *fiber.Ctx) error {
	vols, err := s.dockerSvc.ListVolumes(c.UserContext())
	if err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": err.Error()})
	}

	type volumeWithBackup struct {
		Name               string   `json:"name"`
		Driver             string   `json:"driver"`
		Mountpoint         string   `json:"mountpoint"`
		Scope              string   `json:"scope"`
		CreatedAt          string   `json:"createdAt"`
		AttachedContainers []string `json:"attachedContainers"`
		InUse              bool     `json:"inUse"`
		LastBackupAt       any      `json:"lastBackupAt,omitempty"`
		LastBackupStatus   string   `json:"lastBackupStatus,omitempty"`
	}

	latest := s.latestBackupByTarget("volume")
	out := make([]volumeWithBackup, 0, len(vols))
	for _, v := range vols {
		item := volumeWithBackup{
			Name:               v.Name,
			Driver:             v.Driver,
			Mountpoint:         v.Mountpoint,
			Scope:              v.Scope,
			CreatedAt:          v.CreatedAt,
			AttachedContainers: v.AttachedContainers,
			InUse:              v.InUse,
		}
		if entry, ok := latest[v.Name]; ok {
			item.LastBackupAt = entry.StartedAt
			item.LastBackupStatus = entry.Status
		}
		out = append(out, item)
	}

	return c.JSON(fiber.Map{"volumes": out})
}

// DeleteVolume deletes an unattached Docker named volume.
func (s *Server) DeleteVolume(c *fiber.Ctx) error {
	name := c.Params("name")
	if name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "volume name is required"})
	}
	if err := s.dockerSvc.RemoveVolume(c.UserContext(), name); err != nil {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"deleted": true})
}
