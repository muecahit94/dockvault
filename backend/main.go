package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/muecahit94/dockvault/config"
	"github.com/muecahit94/dockvault/docker"
	"github.com/muecahit94/dockvault/handlers"
	"github.com/muecahit94/dockvault/models"
	"github.com/muecahit94/dockvault/scheduler"
)

func main() {
	cfg := config.Load()

	db, err := models.InitDB(cfg.DataDir, cfg.BackupDir)
	if err != nil {
		log.Fatalf("dockvault: failed to initialize database: %v", err)
	}

	dockerSvc, err := docker.NewService(db, cfg.BackupDir, cfg.HelperImage, cfg.EncryptionKey)
	if err != nil {
		log.Fatalf("dockvault: failed to initialize Docker client: %v", err)
	}
	defer dockerSvc.Close()

	// Clean up any leftover helper containers from an ungraceful shutdown.
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
	dockerSvc.CleanupOrphanHelpers(cleanupCtx)
	cleanupCancel()

	schedMgr := scheduler.NewManager(db, dockerSvc)
	if err := schedMgr.Sync(); err != nil {
		log.Printf("dockvault: warning: failed to sync schedules: %v", err)
	}

	app := handlers.NewApp(cfg, db, dockerSvc, schedMgr)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("dockvault: shutting down gracefully...")
		schedMgr.Stop()
		if err := app.ShutdownWithTimeout(10 * time.Second); err != nil {
			log.Printf("dockvault: server shutdown error: %v", err)
		}
	}()

	addr := ":" + cfg.Port
	log.Printf("dockvault %s listening on http://0.0.0.0%s", cfg.Version, addr)
	if err := app.Listen(addr); err != nil {
		log.Fatalf("dockvault: server stopped with error: %v", err)
	}
}
