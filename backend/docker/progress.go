package docker

import (
	"encoding/json"
	"sync"
	"time"
)

// JobStep records an individual stage in a backup execution timeline.
type JobStep struct {
	Name       string     `json:"name"`
	Status     string     `json:"status"` // "pending", "running", "done", "skipped", "failed"
	Detail     string     `json:"detail,omitempty"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	DurationMs int64      `json:"durationMs,omitempty"`
}

// JobProgress represents live or recently completed backup progress streamed via SSE.
type JobProgress struct {
	BackupID       uint      `json:"backupId"`
	TargetName     string    `json:"targetName"`
	TargetType     string    `json:"targetType"`
	Phase          string    `json:"phase"`
	PhaseLabel     string    `json:"phaseLabel"`
	BytesProcessed int64     `json:"bytesProcessed"`
	BytesPerSec    float64   `json:"bytesPerSec"`
	Status         string    `json:"status"` // "running", "success", "failed"
	Error          string    `json:"error,omitempty"`
	StartedAt      time.Time `json:"startedAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	Steps          []JobStep `json:"steps"`
	Logs           []string  `json:"logs,omitempty"`
}

// ProgressHub coordinates live backup progress snapshots and SSE subscribers.
type ProgressHub struct {
	mu          sync.RWMutex
	jobs        map[uint]*JobProgress
	subscribers map[chan []JobProgress]struct{}
}

// NewProgressHub initializes an empty ProgressHub.
func NewProgressHub() *ProgressHub {
	return &ProgressHub{
		jobs:        make(map[uint]*JobProgress),
		subscribers: make(map[chan []JobProgress]struct{}),
	}
}

// StartJob registers a new running backup job and returns a tracker for updates.
func (h *ProgressHub) StartJob(backupID uint, targetName, targetType string) *JobTracker {
	now := time.Now().UTC()
	job := &JobProgress{
		BackupID:   backupID,
		TargetName: targetName,
		TargetType: targetType,
		Phase:      "prepare",
		PhaseLabel: "Preparing backup",
		Status:     "running",
		StartedAt:  now,
		UpdatedAt:  now,
		Steps:      []JobStep{},
	}

	h.mu.Lock()
	h.pruneExpiredLocked(now)
	h.jobs[backupID] = job
	h.mu.Unlock()
	h.broadcast()

	return &JobTracker{
		hub:         h,
		backupID:    backupID,
		startedAt:   now,
		lastEmit:    now,
		lastLogAt:   now,
	}
}

// Snapshot returns copies of active and recently completed jobs.
func (h *ProgressHub) Snapshot() []JobProgress {
	h.mu.RLock()
	defer h.mu.RUnlock()

	now := time.Now().UTC()
	out := make([]JobProgress, 0, len(h.jobs))
	for _, job := range h.jobs {
		if job.Status != "running" && now.Sub(job.UpdatedAt) > 2*time.Minute {
			continue
		}
		out = append(out, cloneJobProgress(job))
	}
	return out
}

// Get returns a single job's progress snapshot by backup ID.
func (h *ProgressHub) Get(backupID uint) (JobProgress, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	job, ok := h.jobs[backupID]
	if !ok {
		return JobProgress{}, false
	}
	return cloneJobProgress(job), true
}

// Subscribe returns a buffered channel receiving snapshots and an unsubscribe function.
func (h *ProgressHub) Subscribe() (chan []JobProgress, func()) {
	ch := make(chan []JobProgress, 16)
	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()

	unsub := func() {
		h.mu.Lock()
		if _, ok := h.subscribers[ch]; ok {
			delete(h.subscribers, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
	return ch, unsub
}

func (h *ProgressHub) pruneExpiredLocked(now time.Time) {
	for id, j := range h.jobs {
		if j.Status != "running" && now.Sub(j.UpdatedAt) > 2*time.Minute {
			delete(h.jobs, id)
		}
	}
}

func (h *ProgressHub) broadcast() {
	snap := h.Snapshot()
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subscribers {
		select {
		case ch <- snap:
		default:
			// Drop stale frame for slow consumer rather than blocking backup execution.
		}
	}
}

func cloneJobProgress(j *JobProgress) JobProgress {
	cp := *j
	cp.Steps = append([]JobStep(nil), j.Steps...)
	cp.Logs = append([]string(nil), j.Logs...)
	return cp
}

// JobTracker provides thread-safe step and byte-progress updates for one backup job.
type JobTracker struct {
	hub       *ProgressHub
	backupID  uint
	startedAt time.Time
	lastEmit  time.Time
	lastLogAt time.Time
}

func (t *JobTracker) BeginStep(phase, label, detail string) {
	if t == nil || t.hub == nil {
		return
	}
	now := time.Now().UTC()
	t.hub.mu.Lock()
	if job, ok := t.hub.jobs[t.backupID]; ok {
		finishRunningStepsLocked(job, now)
		job.Phase = phase
		job.PhaseLabel = label
		job.UpdatedAt = now
		ts := now
		job.Steps = append(job.Steps, JobStep{
			Name:      label,
			Status:    "running",
			Detail:    detail,
			StartedAt: &ts,
		})
		msg := label
		if detail != "" {
			msg += ": " + detail
		}
		appendLogLocked(job, now, msg)
	}
	t.hub.mu.Unlock()
	t.hub.broadcast()
}

func (t *JobTracker) SkipStep(label, detail string) {
	if t == nil || t.hub == nil {
		return
	}
	now := time.Now().UTC()
	t.hub.mu.Lock()
	if job, ok := t.hub.jobs[t.backupID]; ok {
		ts := now
		job.Steps = append(job.Steps, JobStep{
			Name:      label,
			Status:    "skipped",
			Detail:    detail,
			StartedAt: &ts,
		})
		job.UpdatedAt = now
	}
	t.hub.mu.Unlock()
	t.hub.broadcast()
}

func (t *JobTracker) CompleteCurrentStep(detail string) {
	if t == nil || t.hub == nil {
		return
	}
	now := time.Now().UTC()
	t.hub.mu.Lock()
	if job, ok := t.hub.jobs[t.backupID]; ok {
		for i := range job.Steps {
			if job.Steps[i].Status == "running" {
				job.Steps[i].Status = "done"
				if detail != "" {
					job.Steps[i].Detail = detail
				}
				if job.Steps[i].StartedAt != nil {
					job.Steps[i].DurationMs = now.Sub(*job.Steps[i].StartedAt).Milliseconds()
				}
			}
		}
		job.UpdatedAt = now
	}
	t.hub.mu.Unlock()
	t.hub.broadcast()
}

func (t *JobTracker) AddLog(line string) {
	if t == nil || t.hub == nil || line == "" {
		return
	}
	now := time.Now().UTC()
	t.hub.mu.Lock()
	if job, ok := t.hub.jobs[t.backupID]; ok {
		appendLogLocked(job, now, line)
		job.UpdatedAt = now
	}
	t.hub.mu.Unlock()
	t.hub.broadcast()
}

func (t *JobTracker) UpdateBytes(bytesProcessed int64) {
	if t == nil || t.hub == nil {
		return
	}
	now := time.Now().UTC()
	t.hub.mu.Lock()
	shouldEmit := now.Sub(t.lastEmit) >= 250*time.Millisecond
	if job, ok := t.hub.jobs[t.backupID]; ok {
		job.BytesProcessed = bytesProcessed
		elapsed := now.Sub(t.startedAt).Seconds()
		if elapsed > 0.05 {
			job.BytesPerSec = float64(bytesProcessed) / elapsed
		}
		job.UpdatedAt = now
	}
	if shouldEmit {
		t.lastEmit = now
	}
	t.hub.mu.Unlock()
	if shouldEmit {
		t.hub.broadcast()
	}
}

func (t *JobTracker) Finish(status, errMsg string) string {
	if t == nil || t.hub == nil {
		return ""
	}
	now := time.Now().UTC()
	var stepsJSON string
	t.hub.mu.Lock()
	if job, ok := t.hub.jobs[t.backupID]; ok {
		for i := range job.Steps {
			if job.Steps[i].Status == "running" {
				if status == "failed" {
					job.Steps[i].Status = "failed"
					if errMsg != "" {
						job.Steps[i].Detail = errMsg
					}
				} else {
					job.Steps[i].Status = "done"
				}
				if job.Steps[i].StartedAt != nil {
					job.Steps[i].DurationMs = now.Sub(*job.Steps[i].StartedAt).Milliseconds()
				}
			}
		}
		job.Status = status
		job.Error = errMsg
		if status == "success" {
			job.Phase = "done"
			job.PhaseLabel = "Completed"
			appendLogLocked(job, now, "Backup completed successfully")
		} else {
			job.Phase = "failed"
			job.PhaseLabel = "Failed"
			appendLogLocked(job, now, "Backup failed: "+errMsg)
		}
		job.UpdatedAt = now
		raw, _ := json.Marshal(job.Steps)
		stepsJSON = string(raw)
	}
	t.hub.mu.Unlock()
	t.hub.broadcast()
	return stepsJSON
}

func finishRunningStepsLocked(job *JobProgress, now time.Time) {
	for i := range job.Steps {
		if job.Steps[i].Status == "running" {
			job.Steps[i].Status = "done"
			if job.Steps[i].StartedAt != nil {
				job.Steps[i].DurationMs = now.Sub(*job.Steps[i].StartedAt).Milliseconds()
			}
		}
	}
}

func appendLogLocked(job *JobProgress, ts time.Time, msg string) {
	entry := ts.Format("15:04:05") + " " + msg
	job.Logs = append(job.Logs, entry)
	if len(job.Logs) > 80 {
		job.Logs = job.Logs[len(job.Logs)-80:]
	}
}
