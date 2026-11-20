package scheduler

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/muecahit94/dockvault/docker"
	"github.com/muecahit94/dockvault/models"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

var cronParser = cron.NewParser(
	cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

// Manager manages recurring cron backup jobs backed by SQLite schedules.
type Manager struct {
	db        *gorm.DB
	dockerSvc *docker.Service
	cron      *cron.Cron
	mu        sync.Mutex
	entryIDs  map[uint]cron.EntryID
	stopCh    chan struct{}
	stopOnce  sync.Once
}

// NewManager creates and starts a new cron Manager.
func NewManager(db *gorm.DB, dockerSvc *docker.Service) *Manager {
	c := cron.New(cron.WithParser(cronParser))
	m := &Manager{
		db:        db,
		dockerSvc: dockerSvc,
		cron:      c,
		entryIDs:  make(map[uint]cron.EntryID),
		stopCh:    make(chan struct{}),
	}
	c.Start()
	go m.runStaleMonitor()
	return m
}

// ValidateCron validates a cron expression and returns its next scheduled time.
func ValidateCron(expr string) (time.Time, error) {
	sched, err := cronParser.Parse(expr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cron expression %q: %w", expr, err)
	}
	return sched.Next(time.Now()), nil
}

// Sync reloads all enabled schedules from the database into the cron runner.
func (m *Manager) Sync() error {
	var schedules []models.BackupSchedule
	if err := m.db.Where("enabled = ?", true).Find(&schedules).Error; err != nil {
		return fmt.Errorf("load enabled schedules: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, entryID := range m.entryIDs {
		m.cron.Remove(entryID)
	}
	m.entryIDs = make(map[uint]cron.EntryID, len(schedules))

	for _, sched := range schedules {
		s := sched
		entryID, err := m.cron.AddFunc(s.CronExpr, func() {
			m.runSchedule(s.ID)
		})
		if err != nil {
			log.Printf("dockvault: skip invalid schedule %d (%s): %v", s.ID, s.CronExpr, err)
			continue
		}
		m.entryIDs[s.ID] = entryID
	}

	return nil
}

// TriggerNow runs a schedule immediately in a background goroutine.
func (m *Manager) TriggerNow(scheduleID uint) error {
	var sched models.BackupSchedule
	if err := m.db.First(&sched, scheduleID).Error; err != nil {
		return fmt.Errorf("schedule %d not found: %w", scheduleID, err)
	}
	go m.runSchedule(scheduleID)
	return nil
}

// NextRun returns the next scheduled run time for an active schedule ID.
func (m *Manager) NextRun(scheduleID uint) *time.Time {
	m.mu.Lock()
	entryID, ok := m.entryIDs[scheduleID]
	m.mu.Unlock()
	if !ok {
		return nil
	}
	entry := m.cron.Entry(entryID)
	if entry.ID == 0 || entry.Next.IsZero() {
		return nil
	}
	next := entry.Next
	return &next
}

// Stop halts the cron scheduler and waits for running jobs to complete.
func (m *Manager) Stop() {
	m.stopOnce.Do(func() { close(m.stopCh) })
	ctx := m.cron.Stop()
	<-ctx.Done()
}

func (m *Manager) runSchedule(scheduleID uint) {
	var sched models.BackupSchedule
	if err := m.db.First(&sched, scheduleID).Error; err != nil {
		log.Printf("dockvault: scheduled job %d not found: %v", scheduleID, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	sid := sched.ID
	targets := sched.ResolvedTargets()
	var firstErr error
	for _, tgt := range targets {
		_, err := m.dockerSvc.ExecuteBackup(ctx, docker.BackupRequest{
			TargetType:         tgt.TargetType,
			TargetID:           tgt.TargetID,
			StorageProviderID:  sched.StorageProviderID,
			SecondaryStorageID: sched.SecondaryStorageID,
			PauseContainer:     sched.PauseContainer,
			SelectedMounts:     tgt.SelectedMounts,
			PreCommand:         sched.PreCommand,
			PostCommand:        sched.PostCommand,
			ExcludePatterns:    docker.ParseExcludePatterns(sched.ExcludePatterns),
			Compression:        sched.Compression,
			DBHotDump:          sched.DBHotDump,
			Trigger:            "scheduled",
			ScheduleID:         &sid,
			RetentionPolicy:    sched.RetentionPolicy,
			RetentionCount:     sched.RetentionCount,
			KeepDaily:          sched.KeepDaily,
			KeepWeekly:         sched.KeepWeekly,
			KeepMonthly:        sched.KeepMonthly,
		})
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	now := time.Now().UTC()
	status := "success"
	if firstErr != nil {
		status = "failed"
		log.Printf("dockvault: scheduled backup %q (id=%d) failed: %v", sched.Name, sched.ID, firstErr)
	} else {
		log.Printf("dockvault: scheduled backup %q (id=%d) completed (%d target(s))", sched.Name, sched.ID, len(targets))
	}

	m.db.Model(&models.BackupSchedule{}).Where("id = ?", sched.ID).Updates(map[string]any{
		"last_run_at": &now,
		"last_status": status,
	})
}
