import { useEffect, useState } from 'react'
import { CalendarClock, Check, AlertTriangle } from 'lucide-react'
import { api, formatDateTime } from '../api/client'
import { Button, Input, Label, Modal, Select, Switch } from './ui'

const CRON_PRESETS = [
  { label: 'Daily 02:00', expr: '0 2 * * *' },
  { label: 'Every 6h', expr: '0 */6 * * *' },
  { label: 'Hourly', expr: '0 * * * *' },
  { label: 'Weekly Sun', expr: '0 3 * * 0' },
]

export function ScheduleModal({
  open,
  onClose,
  initialTarget,
  existingSchedule,
  providers,
  containers,
  stacks,
  volumes = [],
  defaultPauseBackups = true,
  onSaved,
  notify,
}) {
  const defaultProvider = providers.find((p) => p.isDefault) || providers[0]

  const [name, setName] = useState('')
  const [targetType, setTargetType] = useState('stack')
  const [targetId, setTargetId] = useState('')
  const [cronExpr, setCronExpr] = useState('0 2 * * *')
  const [storageProviderId, setStorageProviderId] = useState('')
  const [secondaryStorageId, setSecondaryStorageId] = useState('')
  const [retentionPolicy, setRetentionPolicy] = useState('count')
  const [retentionCount, setRetentionCount] = useState(7)
  const [keepDaily, setKeepDaily] = useState(7)
  const [keepWeekly, setKeepWeekly] = useState(4)
  const [keepMonthly, setKeepMonthly] = useState(3)
  const [preCommand, setPreCommand] = useState('')
  const [postCommand, setPostCommand] = useState('')
  const [compression, setCompression] = useState('gzip')
  const [excludePatterns, setExcludePatterns] = useState('')
  const [dbHotDump, setDbHotDump] = useState(false)
  const [pauseContainer, setPauseContainer] = useState(true)
  const [enabled, setEnabled] = useState(true)
  const [alertAfterHours, setAlertAfterHours] = useState(0)
  const [preview, setPreview] = useState({ valid: true, nextRunAt: null, error: '' })
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open) return
    if (existingSchedule) {
      setName(existingSchedule.name)
      setTargetType(existingSchedule.targetType)
      setTargetId(existingSchedule.targetId)
      setCronExpr(existingSchedule.cronExpr)
      setStorageProviderId(String(existingSchedule.storageProviderId))
      setSecondaryStorageId(
        existingSchedule.secondaryStorageId ? String(existingSchedule.secondaryStorageId) : '',
      )
      setRetentionPolicy(existingSchedule.retentionPolicy || 'count')
      setRetentionCount(existingSchedule.retentionCount ?? 7)
      setKeepDaily(existingSchedule.keepDaily ?? 7)
      setKeepWeekly(existingSchedule.keepWeekly ?? 4)
      setKeepMonthly(existingSchedule.keepMonthly ?? 3)
      setPreCommand(existingSchedule.preCommand || '')
      setPostCommand(existingSchedule.postCommand || '')
      setCompression(existingSchedule.compression || 'gzip')
      setExcludePatterns(existingSchedule.excludePatterns || '')
      setDbHotDump(Boolean(existingSchedule.dbHotDump))
      setPauseContainer(existingSchedule.pauseContainer ?? true)
      setEnabled(existingSchedule.enabled ?? true)
      setAlertAfterHours(existingSchedule.alertAfterHours ?? 0)
    } else if (initialTarget) {
      setName(`${initialTarget.targetName} Backup`)
      setTargetType(initialTarget.targetType)
      setTargetId(initialTarget.targetId)
      setCronExpr(initialTarget.gitopsSchedule || '0 2 * * *')
      setStorageProviderId(String(defaultProvider?.id || ''))
      setSecondaryStorageId('')
      setRetentionPolicy('count')
      setRetentionCount(7)
      setKeepDaily(7)
      setKeepWeekly(4)
      setKeepMonthly(3)
      setPreCommand('')
      setPostCommand('')
      setCompression(initialTarget.gitopsCompression || 'gzip')
      setExcludePatterns(initialTarget.gitopsExcludePaths || '')
      setDbHotDump(
        Boolean(initialTarget.detectedDb || initialTarget.detectedDbs?.length),
      )
      setPauseContainer(defaultPauseBackups)
      setEnabled(true)
      setAlertAfterHours(0)
    } else {
      const firstStack = stacks[0]
      const firstContainer = containers.find((c) => c.backupableCount > 0) || containers[0]
      const firstVol = volumes[0]
      const defaultType = firstStack ? 'stack' : firstContainer ? 'container' : 'volume'
      const defaultId = firstStack
        ? firstStack.name
        : firstContainer
          ? firstContainer.name
          : firstVol?.name || ''
      setName(defaultId ? `${defaultId} Backup` : '')
      setTargetType(defaultType)
      setTargetId(defaultId)
      setCronExpr('0 2 * * *')
      setStorageProviderId(String(defaultProvider?.id || ''))
      setSecondaryStorageId('')
      setRetentionPolicy('count')
      setRetentionCount(7)
      setKeepDaily(7)
      setKeepWeekly(4)
      setKeepMonthly(3)
      setPreCommand('')
      setPostCommand('')
      setCompression('gzip')
      setExcludePatterns('')
      setDbHotDump(false)
      setPauseContainer(defaultPauseBackups)
      setEnabled(true)
      setAlertAfterHours(0)
    }
    // Intentionally initialize only when modal opens or target/schedule changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, existingSchedule, initialTarget])

  useEffect(() => {
    if (!open || !cronExpr.trim()) return
    let active = true
    const timer = setTimeout(async () => {
      try {
        const res = await api.previewCron(cronExpr)
        if (active) setPreview({ valid: true, nextRunAt: res.nextRunAt, error: '' })
      } catch (err) {
        if (active) setPreview({ valid: false, nextRunAt: null, error: err.message })
      }
    }, 200)
    return () => {
      active = false
      clearTimeout(timer)
    }
  }, [open, cronExpr])

  const targetOptions =
    targetType === 'stack'
      ? stacks.map((s) => ({ id: s.name, label: `${s.name} (${s.backupableCount} mounts)` }))
      : targetType === 'volume'
        ? volumes.map((v) => ({
            id: v.name,
            label: `${v.name}${v.inUse ? '' : ' (orphan)'}`,
          }))
        : containers
            .filter((c) => c.backupableCount > 0 || c.name === targetId)
            .map((c) => ({ id: c.name, label: `${c.name} (${c.backupableCount} mounts)` }))

  const activePrimaryId = storageProviderId || String(defaultProvider?.id || '')

  async function handleSubmit(e) {
    e.preventDefault()
    if (!targetId) {
      notify('error', 'Please select a target container, stack, or volume')
      return
    }
    setSaving(true)
    try {
      const payload = {
        name: name.trim() || `${targetId} Backup`,
        targetType,
        targetId,
        targetName: targetId,
        cronExpr: cronExpr.trim(),
        storageProviderId: Number(activePrimaryId) || 0,
        secondaryStorageId: secondaryStorageId ? Number(secondaryStorageId) : 0,
        retentionPolicy,
        retentionCount: Number(retentionCount) || 0,
        keepDaily: Number(keepDaily) || 0,
        keepWeekly: Number(keepWeekly) || 0,
        keepMonthly: Number(keepMonthly) || 0,
        preCommand: preCommand.trim(),
        postCommand: postCommand.trim(),
        compression,
        excludePatterns: excludePatterns.trim(),
        dbHotDump,
        pauseContainer,
        enabled,
        alertAfterHours: Math.max(0, Number(alertAfterHours) || 0),
      }
      if (existingSchedule) {
        await api.updateSchedule(existingSchedule.id, payload)
        notify('success', 'Schedule updated')
      } else {
        await api.createSchedule(payload)
        notify('success', 'Schedule created')
      }
      onSaved?.()
      onClose()
    } catch (err) {
      notify('error', err.message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={existingSchedule ? 'Edit Backup Schedule' : 'Create Backup Schedule'}
      subtitle="Automate recurring backups with 3-2-1 offsite copies and GFS retention"
      maxWidth="max-w-xl"
    >
      <form onSubmit={handleSubmit} className="space-y-4 max-h-[80vh] overflow-y-auto pr-1">
        <div>
          <Label htmlFor="sched-name">Schedule Name</Label>
          <Input
            id="sched-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Nightly Postgres Backup"
            required
          />
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
          <div>
            <Label htmlFor="sched-type">Target Type</Label>
            <Select
              id="sched-type"
              value={targetType}
              onChange={(e) => {
                const nextType = e.target.value
                setTargetType(nextType)
                if (nextType === 'stack' && stacks[0]) {
                  setTargetId(stacks[0].name)
                } else if (nextType === 'volume' && volumes[0]) {
                  setTargetId(volumes[0].name)
                } else if (nextType === 'container' && containers[0]) {
                  setTargetId(containers[0].name)
                } else {
                  setTargetId('')
                }
              }}
            >
              <option value="stack">Compose Stack</option>
              <option value="container">Container</option>
              <option value="volume">Docker Volume</option>
            </Select>
          </div>
          <div className="sm:col-span-2">
            <Label htmlFor="sched-target">Target</Label>
            <Select
              id="sched-target"
              value={targetId}
              onChange={(e) => setTargetId(e.target.value)}
            >
              {targetOptions.length === 0 ? (
                <option value="">No targets available</option>
              ) : (
                targetOptions.map((opt) => (
                  <option key={opt.id} value={opt.id}>
                    {opt.label}
                  </option>
                ))
              )}
            </Select>
          </div>
        </div>

        <div>
          <div className="flex items-center justify-between mb-1.5">
            <Label htmlFor="sched-cron" className="mb-0">
              Cron Expression
            </Label>
            <div className="flex flex-wrap gap-1">
              {CRON_PRESETS.map((preset) => (
                <button
                  key={preset.expr}
                  type="button"
                  onClick={() => setCronExpr(preset.expr)}
                  className="rounded-md border border-zinc-700/80 bg-zinc-800/80 px-2 py-0.5 text-[11px] text-zinc-300 hover:border-sky-500/50 hover:text-sky-300 cursor-pointer"
                >
                  {preset.label}
                </button>
              ))}
            </div>
          </div>
          <Input
            id="sched-cron"
            value={cronExpr}
            onChange={(e) => setCronExpr(e.target.value)}
            className="font-mono"
            placeholder="0 2 * * *"
            required
          />
          <div className="mt-1.5 flex items-center gap-1.5 text-xs">
            {preview.valid ? (
              <>
                <Check className="h-3.5 w-3.5 text-emerald-400" />
                <span className="text-zinc-400">
                  Next run: <span className="text-zinc-200">{formatDateTime(preview.nextRunAt)}</span>
                </span>
              </>
            ) : (
              <>
                <AlertTriangle className="h-3.5 w-3.5 text-rose-400" />
                <span className="text-rose-300">{preview.error}</span>
              </>
            )}
          </div>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div>
            <Label htmlFor="sched-storage">Primary Storage</Label>
            <Select
              id="sched-storage"
              value={activePrimaryId}
              onChange={(e) => setStorageProviderId(e.target.value)}
            >
              {providers.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} ({p.type.toUpperCase()}){p.encryptBackups ? ' 🔒' : ''}
                </option>
              ))}
            </Select>
          </div>
          <div>
            <Label htmlFor="sched-sec-storage">Secondary Offsite (3-2-1)</Label>
            <Select
              id="sched-sec-storage"
              value={secondaryStorageId}
              onChange={(e) => setSecondaryStorageId(e.target.value)}
            >
              <option value="">None (single destination)</option>
              {providers
                .filter((p) => String(p.id) !== String(activePrimaryId))
                .map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name} ({p.type.toUpperCase()}){p.encryptBackups ? ' 🔒' : ''}
                  </option>
                ))}
            </Select>
          </div>
        </div>

        {/* Retention Policy */}
        <div className="rounded-xl border border-zinc-800 bg-zinc-950/50 p-3 space-y-3">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 items-end">
            <div>
              <Label htmlFor="sched-ret-policy">Retention Policy</Label>
              <Select
                id="sched-ret-policy"
                value={retentionPolicy}
                onChange={(e) => setRetentionPolicy(e.target.value)}
              >
                <option value="count">Keep Latest N Backups</option>
                <option value="gfs">GFS (Daily / Weekly / Monthly)</option>
              </Select>
            </div>
            {retentionPolicy === 'count' && (
              <div>
                <Label htmlFor="sched-retention">Keep Count (0 = unlimited)</Label>
                <Input
                  id="sched-retention"
                  type="number"
                  min="0"
                  max="365"
                  value={retentionCount}
                  onChange={(e) => setRetentionCount(e.target.value)}
                />
              </div>
            )}
          </div>

          {retentionPolicy === 'gfs' && (
            <div className="grid grid-cols-3 gap-2">
              <div>
                <Label htmlFor="gfs-daily">Daily</Label>
                <Input
                  id="gfs-daily"
                  type="number"
                  min="0"
                  max="90"
                  value={keepDaily}
                  onChange={(e) => setKeepDaily(e.target.value)}
                />
              </div>
              <div>
                <Label htmlFor="gfs-weekly">Weekly</Label>
                <Input
                  id="gfs-weekly"
                  type="number"
                  min="0"
                  max="52"
                  value={keepWeekly}
                  onChange={(e) => setKeepWeekly(e.target.value)}
                />
              </div>
              <div>
                <Label htmlFor="gfs-monthly">Monthly</Label>
                <Input
                  id="gfs-monthly"
                  type="number"
                  min="0"
                  max="60"
                  value={keepMonthly}
                  onChange={(e) => setKeepMonthly(e.target.value)}
                />
              </div>
            </div>
          )}
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div>
            <Label htmlFor="sched-compression">Archive Compression</Label>
            <Select
              id="sched-compression"
              value={compression}
              onChange={(e) => setCompression(e.target.value)}
            >
              <option value="gzip">gzip (.tar.gz — Universal)</option>
              <option value="zstd">zstd (.tar.zst — Faster &amp; Smaller)</option>
            </Select>
          </div>
          <div>
            <Label htmlFor="sched-excludes">Exclude Patterns (optional)</Label>
            <Input
              id="sched-excludes"
              value={excludePatterns}
              onChange={(e) => setExcludePatterns(e.target.value)}
              placeholder="*.log, **/cache/**, node_modules"
              className="font-mono text-xs"
            />
          </div>
        </div>

        {targetType !== 'volume' && (
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div>
              <Label htmlFor="sched-pre">Pre-backup Exec Hook (optional)</Label>
              <Input
                id="sched-pre"
                value={preCommand}
                onChange={(e) => setPreCommand(e.target.value)}
                placeholder="pg_dumpall -U postgres > /data/dump.sql"
                className="font-mono text-xs"
              />
            </div>
            <div>
              <Label htmlFor="sched-post">Post-backup Cleanup (optional)</Label>
              <Input
                id="sched-post"
                value={postCommand}
                onChange={(e) => setPostCommand(e.target.value)}
                placeholder="rm -f /data/dump.sql"
                className="font-mono text-xs"
              />
            </div>
          </div>
        )}

        <div className="space-y-2">
          {targetType !== 'volume' && (
            <>
              <div className="flex items-center justify-between rounded-xl border border-zinc-800 bg-zinc-950/50 px-3.5 py-2.5">
                <span className="text-xs text-zinc-200">
                  Zero-downtime database hot dumps (auto-detect postgres/mysql/mariadb/mongo/redis)
                </span>
                <Switch checked={dbHotDump} onChange={setDbHotDump} />
              </div>
              <div className="flex items-center justify-between rounded-xl border border-zinc-800 bg-zinc-950/50 px-3.5 py-2.5">
                <span className="text-xs text-zinc-200">Pause running container(s) during backup</span>
                <Switch checked={pauseContainer} onChange={setPauseContainer} />
              </div>
            </>
          )}
          <div className="flex items-center justify-between gap-3 rounded-xl border border-zinc-800 bg-zinc-950/50 px-3.5 py-2.5">
            <div className="pr-2">
              <label htmlFor="sched-alert-after" className="text-xs text-zinc-200">
                Alert if no successful backup within (hours)
              </label>
              <p className="text-[11px] text-zinc-400">
                Sends an "overdue" alert to failure channels when backups silently stop. 0 = off; use 26 for a daily job.
              </p>
            </div>
            <Input
              id="sched-alert-after"
              type="number"
              min={0}
              max={8760}
              value={alertAfterHours}
              onChange={(e) => setAlertAfterHours(e.target.value)}
              className="w-24 font-mono text-xs"
            />
          </div>
          <div className="flex items-center justify-between rounded-xl border border-zinc-800 bg-zinc-950/50 px-3.5 py-2.5">
            <span className="text-xs text-zinc-200">Schedule enabled</span>
            <Switch checked={enabled} onChange={setEnabled} />
          </div>
        </div>

        <div className="flex items-center justify-end gap-2 pt-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" disabled={saving || !preview.valid}>
            <CalendarClock className="h-4 w-4" />
            {saving ? 'Saving...' : existingSchedule ? 'Update Schedule' : 'Create Schedule'}
          </Button>
        </div>
      </form>
    </Modal>
  )
}
