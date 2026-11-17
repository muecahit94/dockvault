package scheduler

import (
	"context"
	"log"
	"time"

	"github.com/muecahit94/dockvault/models"
	"github.com/muecahit94/dockvault/notify"
	"gorm.io/gorm"
)

const staleCheckInterval = 5 * time.Minute

// LastSuccessAt returns when a schedule last produced a successful backup, or nil if never.
func LastSuccessAt(db *gorm.DB, scheduleID uint) *time.Time {
	var entry models.BackupLog
	res := db.Where("schedule_id = ? AND status = ?", scheduleID, "success").
		Order("started_at desc").Limit(1).Find(&entry)
	if res.Error != nil || res.RowsAffected == 0 {
		return nil
	}
	if entry.CompletedAt != nil {
		t := *entry.CompletedAt
		return &t
	}
	t := entry.StartedAt
	return &t
}

// IsStale reports whether an enabled schedule with an alert window has gone too long without success.
// Schedules that never succeeded are measured from their creation time.
func IsStale(sched models.BackupSchedule, lastSuccess *time.Time, now time.Time) bool {
	if !sched.Enabled || sched.AlertAfterHours <= 0 {
		return false
	}
	return now.Sub(staleReference(sched, lastSuccess)) > time.Duration(sched.AlertAfterHours)*time.Hour
}

func staleReference(sched models.BackupSchedule, lastSuccess *time.Time) time.Time {
	if lastSuccess != nil && lastSuccess.After(sched.CreatedAt) {
		return *lastSuccess
	}
	return sched.CreatedAt
}

// CheckStaleSchedules alerts once per overdue period for schedules without a recent success.
// It returns the number of schedules for which an alert was delivered.
func (m *Manager) CheckStaleSchedules(ctx context.Context, now time.Time) int {
	var schedules []models.BackupSchedule
	if err := m.db.Where("enabled = ? AND alert_after_hours > ?", true, 0).Find(&schedules).Error; err != nil {
		return 0
	}
	alerted := 0
	for _, sched := range schedules {
		last := LastSuccessAt(m.db, sched.ID)
		if !IsStale(sched, last, now) {
			continue
		}
		if sched.StaleAlertedAt != nil && sched.StaleAlertedAt.After(staleReference(sched, last)) {
			continue
		}
		delivered := notify.DispatchStaleAlert(ctx, m.db, notify.StaleSchedule{
			ScheduleID:      sched.ID,
			Name:            sched.Name,
			TargetType:      sched.TargetType,
			TargetName:      sched.TargetName,
			CronExpr:        sched.CronExpr,
			AlertAfterHours: sched.AlertAfterHours,
			LastSuccessAt:   last,
			LastStatus:      sched.LastStatus,
		})
		if !delivered {
			continue
		}
		alertedAt := now.UTC()
		m.db.Model(&models.BackupSchedule{}).Where("id = ?", sched.ID).Update("stale_alerted_at", &alertedAt)
		log.Printf("dockvault: stale backup alert sent for schedule %q (id=%d)", sched.Name, sched.ID)
		alerted++
	}
	return alerted
}

func (m *Manager) runStaleMonitor() {
	ticker := time.NewTicker(staleCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			m.CheckStaleSchedules(ctx, time.Now())
			cancel()
		}
	}
}
