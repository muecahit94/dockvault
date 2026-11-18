package handlers

import (
	"github.com/gofiber/fiber/v2"
	"github.com/muecahit94/dockvault/models"
)

// ListContainers returns discovered Docker containers and their mount points.
func (s *Server) ListContainers(c *fiber.Ctx) error {
	includeAll := c.Query("all", "true") != "false"
	containers, err := s.dockerSvc.ListContainers(c.UserContext(), includeAll)
	if err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	latest := s.latestBackupByTarget("container")
	restoreState := computeRestoreState(s.db, 200)
	for i := range containers {
		if entry, ok := latest[containers[i].Name]; ok {
			t := entry.StartedAt
			containers[i].LastBackupAt = &t
			containers[i].LastBackupStatus = entry.Status
		}
		containers[i].LastRestore = resolveTargetRestore(
			"container",
			containers[i].Name,
			containers[i].ComposeProject,
			restoreState,
		)
	}

	return c.JSON(fiber.Map{
		"containers": containers,
	})
}

// ListStacks returns containers grouped by Docker Compose project.
func (s *Server) ListStacks(c *fiber.Ctx) error {
	includeAll := c.Query("all", "true") != "false"
	stacks, err := s.dockerSvc.ListStacks(c.UserContext(), includeAll)
	if err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	latest := s.latestBackupByTarget("stack")
	restoreState := computeRestoreState(s.db, 200)
	for i := range stacks {
		if entry, ok := latest[stacks[i].Name]; ok {
			t := entry.StartedAt
			stacks[i].LastBackupAt = &t
			stacks[i].LastBackupStatus = entry.Status
		}
		stacks[i].LastRestore = resolveTargetRestore("stack", stacks[i].Name, "", restoreState)
	}

	return c.JSON(fiber.Map{
		"stacks": stacks,
	})
}

func (s *Server) latestBackupByTarget(targetType string) map[string]models.BackupLog {
	var logs []models.BackupLog
	s.db.Where("target_type = ? AND status IN ?", targetType, []string{"success", "failed"}).
		Order("started_at desc").
		Limit(250).
		Find(&logs)

	out := make(map[string]models.BackupLog, len(logs))
	for _, entry := range logs {
		if _, exists := out[entry.TargetName]; !exists {
			out[entry.TargetName] = entry
		}
	}
	return out
}
