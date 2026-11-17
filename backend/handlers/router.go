package handlers

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/muecahit94/dockvault/config"
	"github.com/muecahit94/dockvault/docker"
	"github.com/muecahit94/dockvault/scheduler"
	"github.com/muecahit94/dockvault/static"
	"gorm.io/gorm"
)

// Server bundles dependencies shared across HTTP handlers.
type Server struct {
	cfg       *config.Config
	db        *gorm.DB
	dockerSvc *docker.Service
	schedMgr  *scheduler.Manager
}

// NewApp wires up the Fiber application, API routes, auth middleware, security headers, and embedded SPA.
func NewApp(cfg *config.Config, db *gorm.DB, dockerSvc *docker.Service, schedMgr *scheduler.Manager) *fiber.App {
	s := &Server{
		cfg:       cfg,
		db:        db,
		dockerSvc: dockerSvc,
		schedMgr:  schedMgr,
	}
	s.bootstrapAuth()

	app := fiber.New(fiber.Config{
		AppName:               "DockVault",
		DisableStartupMessage: true,
	})

	app.Use(recover.New())
	app.Use(logger.New(logger.Config{
		Format: "${time} | ${status} | ${latency} | ${method} ${path}\n",
	}))
	app.Use(securityHeaders(cfg.FrameAncestors))

	api := app.Group("/api")

	// Public endpoints (healthcheck & auth handshake).
	api.Get("/health", s.GetHealth)
	api.Get("/auth/status", s.GetAuthStatus)
	api.Post("/auth/setup", s.SetupAuth)
	api.Post("/auth/login", s.Login)
	api.Post("/auth/logout", s.Logout)

	// Protected API endpoints when built-in auth is enabled.
	protected := api.Group("", s.AuthMiddleware())
	protected.Put("/auth/settings", s.UpdateAuthSettings)

	protected.Get("/dashboard", s.GetDashboard)
	protected.Get("/containers", s.ListContainers)
	protected.Get("/stacks", s.ListStacks)
	protected.Get("/volumes", s.ListVolumes)
	protected.Delete("/volumes/:name", s.DeleteVolume)

	protected.Get("/backups", s.ListBackups)
	protected.Get("/backups/events", s.StreamBackupEvents)
	protected.Post("/backups/run", s.RunBackup)
	protected.Get("/backups/:id/progress", s.GetBackupProgress)
	protected.Get("/backups/:id/download", s.DownloadBackup)
	protected.Get("/backups/:id/inspect", s.InspectBackup)
	protected.Get("/backups/:id/file", s.ExtractSingleFile)
	protected.Post("/backups/:id/verify", s.VerifyBackup)
	protected.Post("/backups/:id/restore", s.RestoreBackup)
	protected.Post("/backups/:id/pin", s.TogglePinBackup)
	protected.Post("/backups/:id/sas", s.CreateBackupSASLink)
	protected.Delete("/backups/:id", s.DeleteBackup)

	protected.Get("/schedules", s.ListSchedules)
	protected.Post("/schedules", s.CreateSchedule)
	protected.Post("/schedules/sync-labels", s.SyncGitOpsSchedules)
	protected.Post("/schedules/preview", s.PreviewCron)
	protected.Put("/schedules/:id", s.UpdateSchedule)
	protected.Post("/schedules/:id/toggle", s.ToggleSchedule)
	protected.Post("/schedules/:id/run", s.TriggerSchedule)
	protected.Delete("/schedules/:id", s.DeleteSchedule)

	protected.Get("/storage", s.ListStorageProviders)
	protected.Post("/storage", s.CreateStorageProvider)
	protected.Post("/storage/test", s.TestUnsavedStorageProvider)
	protected.Put("/storage/:id", s.UpdateStorageProvider)
	protected.Delete("/storage/:id", s.DeleteStorageProvider)
	protected.Post("/storage/:id/test", s.TestStorageProvider)
	protected.Post("/storage/:id/scan", s.ScanStorageProvider)
	protected.Post("/storage/:id/import", s.ImportStorageBackups)

	protected.Get("/notifications", s.GetNotifications)
	protected.Post("/notifications", s.CreateNotification)
	protected.Put("/notifications", s.UpdateNotifications)
	protected.Post("/notifications/test", s.TestNotifications)
	protected.Put("/notifications/:id", s.UpdateNotification)
	protected.Post("/notifications/:id/toggle", s.ToggleNotification)
	protected.Post("/notifications/:id/test", s.TestNotificationByID)
	protected.Delete("/notifications/:id", s.DeleteNotification)

	protected.Get("/settings/preferences", s.GetUIPreferences)
	protected.Put("/settings/preferences", s.UpdateUIPreferences)
	protected.Post("/settings/preferences/reset", s.ResetUIPreferences)

	// Serve embedded React frontend with SPA fallback for client-side routes.
	distFS, err := fs.Sub(static.DistFS, "dist")
	if err == nil {
		app.Use("/", filesystem.New(filesystem.Config{
			Root:         http.FS(distFS),
			Browse:       false,
			Index:        "index.html",
			NotFoundFile: "index.html",
		}))
	}

	return app
}

func securityHeaders(rawFrameAncestors string) fiber.Handler {
	cleaned := strings.ReplaceAll(rawFrameAncestors, ",", " ")
	ancestors := strings.Fields(cleaned)
	var cspFrameAncestors string
	if len(ancestors) > 0 {
		// Prevent semicolon injection into the CSP header value.
		safe := make([]string, 0, len(ancestors))
		for _, a := range ancestors {
			if !strings.ContainsAny(a, ";\r\n") {
				safe = append(safe, a)
			}
		}
		if len(safe) > 0 {
			cspFrameAncestors = "frame-ancestors 'self' " + strings.Join(safe, " ")
		}
	}

	return func(c *fiber.Ctx) error {
		c.Set("X-Content-Type-Options", "nosniff")
		if cspFrameAncestors != "" {
			c.Set("Content-Security-Policy", cspFrameAncestors)
		} else {
			c.Set("X-Frame-Options", "DENY")
		}
		c.Set("Referrer-Policy", "no-referrer")
		c.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		return c.Next()
	}
}
