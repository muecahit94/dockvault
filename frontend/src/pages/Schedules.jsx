import { useMemo, useState } from 'react'
import { AlertTriangle, CalendarClock, Clock, Copy, CopyPlus, Edit2, Play, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { api, formatDateTime, formatRelativeTime } from '../api/client'
import { ApplyScheduleModal } from '../components/ApplyScheduleModal'
import { Badge, Button, Card, EmptyState, Switch } from '../components/ui'

function extractCronHours(expr) {
  const raw = (expr || '').trim().toLowerCase()
  if (!raw) return []
  if (raw === '@hourly' || raw === '@every 1h') {
    return Array.from({ length: 24 }, (_, i) => i)
  }
  if (raw === '@daily' || raw === '@midnight') return [0]
  if (raw.startsWith('@every ')) {
    const m = raw.match(/@every\s+(\d+)h/)
    if (m) {
      const step = Math.max(1, Number(m[1]))
      return Array.from({ length: 24 }, (_, i) => i).filter((h) => h % step === 0)
    }
    return []
  }
  const fields = raw.split(/\s+/)
  if (fields.length < 5) return []
  const hourField = fields[1]
  if (hourField === '*') return Array.from({ length: 24 }, (_, i) => i)
  const hours = new Set()
  for (const part of hourField.split(',')) {
    if (part.startsWith('*/')) {
      const step = Math.max(1, Number(part.slice(2)))
      for (let h = 0; h < 24; h += step) hours.add(h)
    } else if (part.includes('-')) {
      const [a, b] = part.split('-').map(Number)
      if (!Number.isNaN(a) && !Number.isNaN(b)) {
        for (let h = Math.max(0, a); h <= Math.min(23, b); h++) hours.add(h)
      }
    } else {
      const n = Number(part)
      if (!Number.isNaN(n) && n >= 0 && n <= 23) hours.add(n)
    }
  }
  return Array.from(hours).sort((a, b) => a - b)
}

export function Schedules({
  schedules,
  stacks = [],
  containers = [],
  volumes = [],
  onCreate,
  onEdit,
  onRefresh,
  notify,
}) {
  const [syncingLabels, setSyncingLabels] = useState(false)
  const [applySource, setApplySource] = useState(null)
  const staleSchedules = schedules.filter((s) => s.stale)

  const timelineHours = useMemo(() => {
    const buckets = Array.from({ length: 24 }, (_, hour) => ({ hour, jobs: [] }))
    for (const s of schedules) {
      if (!s.enabled) continue
      const hrs = extractCronHours(s.cronExpr)
      for (const h of hrs) {
        buckets[h].jobs.push(s)
      }
    }
    return buckets
  }, [schedules])

  const overlappingHours = timelineHours.filter((b) => b.jobs.length > 1)

  async function handleSyncGitOps() {
    setSyncingLabels(true)
    try {
      const res = await api.syncGitOpsSchedules()
      notify(
        'success',
        `GitOps label sync: ${res.created || 0} created, ${res.updated || 0} updated, ${res.skipped || 0} skipped`,
      )
      onRefresh()
    } catch (err) {
      notify('error', `GitOps sync failed: ${err.message}`)
    } finally {
      setSyncingLabels(false)
    }
  }
  async function handleToggle(sched) {
    try {
      await api.toggleSchedule(sched.id)
      notify('success', `Schedule "${sched.name}" ${sched.enabled ? 'paused' : 'enabled'}`)
      onRefresh()
    } catch (err) {
      notify('error', err.message)
    }
  }

  async function handleTrigger(sched) {
    try {
      await api.triggerSchedule(sched.id)
      notify('success', `Triggered "${sched.name}"`)
      setTimeout(onRefresh, 600)
    } catch (err) {
      notify('error', err.message)
    }
  }

  async function handleDuplicate(sched) {
    try {
      const res = await api.cloneSchedule(sched.id)
      const copy = res.schedules?.[0]
      notify('success', `Created paused copy "${copy?.name || sched.name}" — edit it, then enable`)
      onRefresh()
      if (copy) onEdit(copy)
    } catch (err) {
      notify('error', err.message)
    }
  }

  async function handleDelete(sched) {
    if (!window.confirm(`Delete schedule "${sched.name}"?`)) return
    try {
      await api.deleteSchedule(sched.id)
      notify('success', `Deleted "${sched.name}"`)
      onRefresh()
    } catch (err) {
      notify('error', err.message)
    }
  }

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-lg font-semibold text-zinc-100">Backup Schedules</h2>
          <p className="text-xs text-zinc-400">
            Automated cron jobs with configurable retention policies &amp; GitOps label sync
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={handleSyncGitOps} disabled={syncingLabels}>
            <RefreshCw className={`h-3.5 w-3.5 ${syncingLabels ? 'animate-spin' : ''}`} />
            {syncingLabels ? 'Syncing...' : 'Sync GitOps Labels'}
          </Button>
          <Button onClick={onCreate}>
            <Plus className="h-4 w-4" />
            New Schedule
          </Button>
        </div>
      </div>

      {staleSchedules.length > 0 && (
        <div className="flex items-start gap-2.5 rounded-xl border border-amber-500/30 bg-amber-500/10 p-3 text-xs text-amber-200">
          <AlertTriangle className="h-4 w-4 shrink-0 mt-0.5 text-amber-400" />
          <div>
            <span className="font-semibold">{staleSchedules.length} schedule(s) overdue:</span>{' '}
            {staleSchedules.map((s) => s.name).join(', ')} — no successful backup within the configured alert window.
          </div>
        </div>
      )}

      {schedules.length > 0 && (
        <Card className="p-4 space-y-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <Clock className="h-4 w-4 text-sky-400" />
              <div>
                <h3 className="text-xs font-semibold text-zinc-100">24-Hour Cron Schedule Timeline</h3>
                <p className="text-[11px] text-zinc-400">
                  Hourly run distribution for active jobs to help avoid backup I/O collisions
                </p>
              </div>
            </div>
            {overlappingHours.length > 0 ? (
              <Badge variant="warning">
                {overlappingHours.length} overlapping hour(s):{' '}
                {overlappingHours.map((b) => `${String(b.hour).padStart(2, '0')}:00`).join(', ')}
              </Badge>
            ) : (
              <Badge variant="success">No hourly collisions</Badge>
            )}
          </div>
          <div className="grid grid-cols-12 sm:grid-cols-24 gap-1">
            {timelineHours.map((slot) => {
              const count = slot.jobs.length
              const title =
                count > 0
                  ? `${String(slot.hour).padStart(2, '0')}:00 — ${slot.jobs.map((j) => j.name).join(', ')}`
                  : `${String(slot.hour).padStart(2, '0')}:00 — idle`
              return (
                <div
                  key={slot.hour}
                  title={title}
                  className="flex flex-col items-center gap-1"
                >
                  <div
                    className={`h-7 w-full rounded border flex items-center justify-center font-mono text-[10px] ${
                      count === 0
                        ? 'border-zinc-800/80 bg-zinc-950/60 text-zinc-600'
                        : count === 1
                          ? 'border-emerald-500/40 bg-emerald-500/15 text-emerald-300 font-semibold'
                          : 'border-amber-500/50 bg-amber-500/20 text-amber-300 font-semibold'
                    }`}
                  >
                    {count > 0 ? count : ''}
                  </div>
                  <span className="font-mono text-[9px] text-zinc-500">
                    {slot.hour % 3 === 0 ? String(slot.hour).padStart(2, '0') : ''}
                  </span>
                </div>
              )
            })}
          </div>
        </Card>
      )}

      {schedules.length === 0 ? (
        <EmptyState
          icon={CalendarClock}
          title="No schedules configured"
          description="Set up recurring cron schedules to automatically pause, archive, and upload container or stack volumes."
          action={
            <Button onClick={onCreate}>
              <Plus className="h-4 w-4" />
              Create First Schedule
            </Button>
          }
        />
      ) : (
        <Card className="overflow-hidden">
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-zinc-800 bg-zinc-950/70 text-xs uppercase tracking-wider text-zinc-400">
                <tr>
                  <th className="px-4 py-3 font-medium">Enabled</th>
                  <th className="px-4 py-3 font-medium">Schedule</th>
                  <th className="px-4 py-3 font-medium">Target</th>
                  <th className="px-4 py-3 font-medium">Cron & Next Run</th>
                  <th className="px-4 py-3 font-medium">Destination</th>
                  <th className="px-4 py-3 font-medium">Retention</th>
                  <th className="px-4 py-3 font-medium text-right">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-zinc-800/70">
                {schedules.map((s) => (
                  <tr key={s.id} className="hover:bg-zinc-900/50">
                    <td className="px-4 py-3.5">
                      <Switch checked={s.enabled} onChange={() => handleToggle(s)} />
                    </td>
                    <td className="px-4 py-3.5">
                      <div className="flex flex-wrap items-center gap-1.5">
                        <span className="font-medium text-zinc-100">{s.name}</span>
                        {s.managedByLabel && (
                          <Badge variant="info" className="text-[10px] py-0 px-1.5">
                            GitOps
                          </Badge>
                        )}
                        {s.compression === 'zstd' && (
                          <Badge variant="neutral" className="text-[10px] py-0 px-1.5">
                            ZSTD
                          </Badge>
                        )}
                        {s.dbHotDump && (
                          <Badge variant="success" className="text-[10px] py-0 px-1.5">
                            DB Dump
                          </Badge>
                        )}
                        {s.stale && (
                          <Badge variant="warning" className="text-[10px] py-0 px-1.5">
                            <AlertTriangle className="h-2.5 w-2.5" />
                            Overdue
                          </Badge>
                        )}
                      </div>
                      <div className="mt-0.5 text-xs text-zinc-500">
                        {s.lastRunAt ? (
                          <span>
                            Last: {formatDateTime(s.lastRunAt)} ({s.lastStatus || 'done'})
                          </span>
                        ) : (
                          'Never run yet'
                        )}
                      </div>
                      {s.alertAfterHours > 0 && (
                        <div className="mt-0.5 text-[11px] text-zinc-500">
                          Last success: {s.lastSuccessAt ? formatRelativeTime(s.lastSuccessAt) : 'never'} · alert after{' '}
                          {s.alertAfterHours}h
                        </div>
                      )}
                    </td>
                    <td className="px-4 py-3.5">
                      <div className="flex flex-wrap items-center gap-1.5">
                        {(s.targets?.length
                          ? s.targets
                          : [{ targetType: s.targetType, targetId: s.targetId, targetName: s.targetName }]
                        ).map((t) => (
                          <span
                            key={`${t.targetType}:${t.targetId}`}
                            className="inline-flex items-center gap-1 rounded-md border border-zinc-800 bg-zinc-950/70 px-2 py-0.5 text-xs text-zinc-200"
                          >
                            <Badge variant="info" className="text-[10px] py-0 px-1">
                              {t.targetType}
                            </Badge>
                            <span>{t.targetName}</span>
                          </span>
                        ))}
                      </div>
                    </td>
                    <td className="px-4 py-3.5">
                      <div className="font-mono text-xs text-sky-400">{s.cronExpr}</div>
                      <div className="mt-0.5 text-xs text-zinc-400">
                        {s.enabled && s.nextRunAt ? formatRelativeTime(s.nextRunAt) : 'Paused'}
                      </div>
                    </td>
                    <td className="px-4 py-3.5 text-xs text-zinc-300">
                      <div>{s.storageProvider?.name || `Provider #${s.storageProviderId}`}</div>
                      {s.secondaryStorageProvider && (
                        <div className="mt-0.5 text-[11px] text-sky-400">
                          + {s.secondaryStorageProvider.name} (3-2-1)
                        </div>
                      )}
                    </td>
                    <td className="px-4 py-3.5 text-xs text-zinc-300">
                      {s.retentionPolicy === 'gfs'
                        ? `GFS (${s.keepDaily}d / ${s.keepWeekly}w / ${s.keepMonthly}m)`
                        : s.retentionCount > 0
                          ? `Keep ${s.retentionCount}`
                          : 'Unlimited'}
                    </td>
                    <td className="px-4 py-3.5 text-right">
                      <div className="inline-flex items-center gap-1.5">
                        <Button
                          variant="outline"
                          size="sm"
                          title="Run now"
                          onClick={() => handleTrigger(s)}
                        >
                          <Play className="h-3.5 w-3.5" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          title="Edit"
                          onClick={() => onEdit(s)}
                        >
                          <Edit2 className="h-3.5 w-3.5" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          title="Duplicate (creates a paused copy)"
                          onClick={() => handleDuplicate(s)}
                        >
                          <Copy className="h-3.5 w-3.5" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          title="Apply this policy to other targets"
                          onClick={() => setApplySource(s)}
                        >
                          <CopyPlus className="h-3.5 w-3.5" />
                        </Button>
                        <Button
                          variant="danger"
                          size="sm"
                          title="Delete"
                          onClick={() => handleDelete(s)}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      )}

      <ApplyScheduleModal
        open={Boolean(applySource)}
        onClose={() => setApplySource(null)}
        source={applySource}
        schedules={schedules}
        stacks={stacks}
        containers={containers}
        volumes={volumes}
        onApplied={onRefresh}
        notify={notify}
      />
    </div>
  )
}
