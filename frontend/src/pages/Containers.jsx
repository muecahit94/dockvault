import { useMemo, useState } from 'react'
import {
  Archive,
  CalendarClock,
  CalendarPlus,
  Container,
  Database,
  HardDrive,
  Layers,
  PauseCircle,
  RefreshCw,
  RotateCcw,
  Search,
  ShieldAlert,
  Terminal,
  Trash2,
} from 'lucide-react'
import { api, formatDateTime, formatRelativeTime } from '../api/client'
import { Badge, Button, Card, EmptyState, Input, Switch } from '../components/ui'

function stateVariant(state) {
  if (state === 'running') return 'success'
  if (state === 'paused') return 'warning'
  return 'neutral'
}

function scheduleListFor(map, ...keys) {
  const seen = new Set()
  const out = []
  for (const k of keys) {
    if (!k) continue
    for (const s of map.get(k) || []) {
      if (!seen.has(s.id)) {
        seen.add(s.id)
        out.push(s)
      }
    }
  }
  return out
}

function SchedulePill({ schedule, viaStack = '', targetName = '', onEdit }) {
  const matchedTarget = (schedule.targets || []).find(
    (t) =>
      (viaStack ? t.targetType === 'stack' && (t.targetId === viaStack || t.targetName === viaStack) : t.targetId === targetName || t.targetName === targetName),
  )
  const mountCount = matchedTarget?.selectedMounts?.length || 0
  const titleParts = [
    schedule.name,
    `Cron: ${schedule.cronExpr}`,
    schedule.enabled
      ? schedule.nextRunAt
        ? `Next run: ${formatDateTime(schedule.nextRunAt)}`
        : 'Enabled'
      : 'Paused',
    viaStack ? `Inherited from stack "${viaStack}"` : '',
    mountCount > 0 ? `Selected volumes: ${matchedTarget.selectedMounts.join(', ')}` : '',
  ].filter(Boolean)

  return (
    <button
      type="button"
      onClick={() => onEdit?.(schedule)}
      title={titleParts.join(' · ')}
      className="inline-flex cursor-pointer items-center"
    >
      <Badge variant={schedule.enabled ? 'info' : 'warning'} className="hover:brightness-110">
        {schedule.enabled ? (
          <CalendarClock className="h-3 w-3 shrink-0" />
        ) : (
          <PauseCircle className="h-3 w-3 shrink-0" />
        )}
        <span>
          {viaStack ? `${viaStack}: ` : ''}
          {schedule.name}
        </span>
        <span className="font-mono text-[10px] opacity-90">{schedule.cronExpr}</span>
        {mountCount > 0 && (
          <span className="text-[10px] opacity-90">({mountCount} vol)</span>
        )}
        {!schedule.enabled && <span className="opacity-90">(Paused)</span>}
      </Badge>
    </button>
  )
}

export function Containers({
  containers,
  stacks,
  volumes = [],
  schedules = [],
  loading,
  defaultTab = 'stacks',
  includeStopped,
  onToggleIncludeStopped,
  onRefresh,
  onOpenBackup,
  onOpenSchedule,
  notify,
}) {
  const [view, setView] = useState(defaultTab || 'stacks')
  const [query, setQuery] = useState('')

  const schedulesByType = useMemo(() => {
    const maps = {
      stack: new Map(),
      container: new Map(),
      volume: new Map(),
    }
    const add = (map, key, sched) => {
      if (!key) return
      const existing = map.get(key) || []
      existing.push(sched)
      map.set(key, existing)
    }
    for (const s of schedules) {
      const targets = s.targets?.length
        ? s.targets
        : [{ targetType: s.targetType, targetId: s.targetId, targetName: s.targetName }]
      for (const tgt of targets) {
        const targetMap = maps[tgt.targetType]
        if (!targetMap) continue
        add(targetMap, tgt.targetId, s)
        if (tgt.targetName && tgt.targetName !== tgt.targetId) {
          add(targetMap, tgt.targetName, s)
        }
      }
    }
    return maps
  }, [schedules])

  const filteredStacks = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return stacks
    return stacks.filter(
      (s) =>
        s.name.toLowerCase().includes(q) ||
        s.containers.some((c) => c.name.toLowerCase().includes(q)),
    )
  }, [stacks, query])

  const filteredContainers = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return containers
    return containers.filter(
      (c) =>
        c.name.toLowerCase().includes(q) ||
        c.image.toLowerCase().includes(q) ||
        (c.composeProject || '').toLowerCase().includes(q),
    )
  }, [containers, query])

  const filteredVolumes = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return volumes
    return volumes.filter(
      (v) =>
        v.name.toLowerCase().includes(q) ||
        (v.attachedContainers || []).some((c) => c.toLowerCase().includes(q)),
    )
  }, [volumes, query])

  async function handleDeleteVolume(vol) {
    if (!window.confirm(`Permanently delete unattached Docker volume "${vol.name}"?`)) return
    try {
      await api.deleteVolume(vol.name)
      notify('success', `Deleted volume "${vol.name}"`)
      onRefresh()
    } catch (err) {
      notify('error', err.message)
    }
  }

  return (
    <div className="space-y-5">
      {/* Controls bar */}
      <Card className="p-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex flex-wrap items-center gap-1 rounded-lg border border-zinc-800 bg-zinc-950 p-1">
            <button
              type="button"
              onClick={() => setView('stacks')}
              className={`flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition-colors cursor-pointer ${
                view === 'stacks'
                  ? 'bg-zinc-800 text-zinc-100'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              <Layers className="h-3.5 w-3.5" />
              Compose Stacks ({stacks.length})
            </button>
            <button
              type="button"
              onClick={() => setView('containers')}
              className={`flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition-colors cursor-pointer ${
                view === 'containers'
                  ? 'bg-zinc-800 text-zinc-100'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              <Container className="h-3.5 w-3.5" />
              Containers ({containers.length})
            </button>
            <button
              type="button"
              onClick={() => setView('volumes')}
              className={`flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition-colors cursor-pointer ${
                view === 'volumes'
                  ? 'bg-zinc-800 text-zinc-100'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              <HardDrive className="h-3.5 w-3.5" />
              Volumes ({volumes.length})
            </button>
          </div>

          <div className="flex flex-wrap items-center gap-3">
            <div className="relative w-64">
              <Search className="pointer-events-none absolute left-3 top-2.5 h-4 w-4 text-zinc-500" />
              <Input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Search stacks, containers, volumes..."
                className="pl-9 h-9"
              />
            </div>

            {view !== 'volumes' && (
              <label className="flex items-center gap-2 text-xs text-zinc-300 cursor-pointer">
                <Switch checked={includeStopped} onChange={onToggleIncludeStopped} />
                Show stopped
              </label>
            )}

            <Button variant="outline" size="sm" onClick={onRefresh} disabled={loading}>
              <RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
              Refresh
            </Button>
          </div>
        </div>
      </Card>

      {/* Stacks View */}
      {view === 'stacks' && (
        <>
          {filteredStacks.length === 0 ? (
            <EmptyState
              icon={Layers}
              title="No Compose stacks found"
              description="Containers launched with Docker Compose (com.docker.compose.project) appear here grouped with their shared volumes."
              action={
                <Button variant="outline" size="sm" onClick={() => setView('containers')}>
                  View Individual Containers
                </Button>
              }
            />
          ) : (
            <div className="grid grid-cols-1 gap-4">
              {filteredStacks.map((stack) => {
                const stackSchedules = scheduleListFor(schedulesByType.stack, stack.name)
                return (
                <Card key={stack.name} className="p-5">
                  <div className="flex flex-wrap items-start justify-between gap-4">
                    <div>
                      <div className="flex flex-wrap items-center gap-2.5">
                        <Layers className="h-4 w-4 text-violet-400" />
                        <h3 className="text-base font-semibold text-zinc-100">{stack.name}</h3>
                        <Badge variant="success">
                          {stack.runningCount}/{stack.totalCount} running
                        </Badge>
                        <Badge variant="info">{stack.backupableCount} backupable mounts</Badge>
                        {stack.detectedDbs?.map((db) => (
                          <Badge key={db} variant="success" className="text-[10px]">
                            <Database className="h-2.5 w-2.5" />
                            {db}
                          </Badge>
                        ))}
                        {stack.gitopsSchedule && (
                          <Badge variant="info" className="font-mono text-[10px]">
                            <CalendarClock className="h-2.5 w-2.5" />
                            GitOps: {stack.gitopsSchedule}
                          </Badge>
                        )}
                        {stackSchedules.length > 0 ? (
                          stackSchedules.map((sched) => (
                            <SchedulePill
                              key={sched.id}
                              schedule={sched}
                              targetName={stack.name}
                              onEdit={(s) => onOpenSchedule(null, s)}
                            />
                          ))
                        ) : (
                          <Badge variant="warning">No schedule</Badge>
                        )}
                        {stack.lastBackupAt ? (
                          <Badge variant={stack.lastBackupStatus === 'failed' ? 'danger' : 'neutral'}>
                            Last backup {formatRelativeTime(stack.lastBackupAt)}
                          </Badge>
                        ) : (
                          <Badge variant="warning">Never backed up</Badge>
                        )}
                        {stack.lastRestore?.isActiveRollback && (
                          <Badge
                            variant="info"
                            title={`Rolled back to backup #${stack.lastRestore.backupLogId} (${stack.lastRestore.backupFileName}) on ${formatDateTime(stack.lastRestore.restoredAt)}`}
                          >
                            <RotateCcw className="h-2.5 w-2.5" />
                            Rolled back → #{stack.lastRestore.backupLogId} ({formatDateTime(stack.lastRestore.backupStartedAt)})
                          </Badge>
                        )}
                      </div>
                      {stack.configFiles && (
                        <p className="mt-1 font-mono text-xs text-zinc-400">
                          Compose: {stack.configFiles}
                        </p>
                      )}
                    </div>

                    <div className="flex items-center gap-2">
                      <Button
                        variant="outline"
                        size="sm"
                        disabled={stack.backupableCount === 0}
                        onClick={() =>
                          onOpenSchedule({
                            targetType: 'stack',
                            targetId: stack.name,
                            targetName: stack.name,
                            detectedDbs: stack.detectedDbs,
                            gitopsSchedule: stack.gitopsSchedule,
                            gitopsExcludePaths: stack.gitopsExcludePaths,
                            gitopsCompression: stack.gitopsCompression,
                            gitopsDbHotDump: stack.gitopsDbHotDump,
                          })
                        }
                      >
                        <CalendarPlus className="h-3.5 w-3.5" />
                        Schedule
                      </Button>
                      <Button
                        size="sm"
                        disabled={stack.backupableCount === 0}
                        onClick={() =>
                          onOpenBackup({
                            targetType: 'stack',
                            targetId: stack.name,
                            targetName: stack.name,
                            mounts: stack.uniqueMounts,
                            detectedDbs: stack.detectedDbs,
                            gitopsExcludePaths: stack.gitopsExcludePaths,
                            gitopsCompression: stack.gitopsCompression,
                            gitopsDbHotDump: stack.gitopsDbHotDump,
                          })
                        }
                      >
                        <Archive className="h-3.5 w-3.5" />
                        Run Backup
                      </Button>
                    </div>
                  </div>

                  {/* Services & Mounts */}
                  <div className="mt-4 grid grid-cols-1 lg:grid-cols-2 gap-4">
                    <div className="rounded-xl border border-zinc-800/80 bg-zinc-950/60 p-3">
                      <div className="text-[11px] font-medium uppercase tracking-wider text-zinc-400 mb-2">
                        Services ({stack.containers.length})
                      </div>
                      <div className="space-y-1.5">
                        {stack.containers.map((c) => {
                          const directSchedules = scheduleListFor(
                            schedulesByType.container,
                            c.id,
                            c.name,
                          )
                          return (
                            <div
                              key={c.id}
                              className="flex items-center justify-between gap-2 text-xs"
                            >
                              <div className="flex flex-wrap items-center gap-2 min-w-0">
                                <span className="truncate font-medium text-zinc-200">
                                  {c.composeService || c.name}
                                </span>
                                <span className="truncate font-mono text-[11px] text-zinc-500">
                                  {c.image}
                                </span>
                                {(c.preHook || c.postHook) && (
                                  <Badge variant="info" className="text-[10px] py-0">
                                    <Terminal className="h-2.5 w-2.5" />
                                    Hook
                                  </Badge>
                                )}
                                {directSchedules.map((sched) => (
                                  <SchedulePill
                                    key={sched.id}
                                    schedule={sched}
                                    targetName={c.name}
                                    onEdit={(s) => onOpenSchedule(null, s)}
                                  />
                                ))}
                              </div>
                              <Badge variant={stateVariant(c.state)}>{c.state}</Badge>
                            </div>
                          )
                        })}
                      </div>
                    </div>

                    <div className="rounded-xl border border-zinc-800/80 bg-zinc-950/60 p-3">
                      <div className="text-[11px] font-medium uppercase tracking-wider text-zinc-400 mb-2">
                        Unique Volumes &amp; Bind Mounts ({stack.uniqueMounts?.length || 0})
                      </div>
                      {!stack.uniqueMounts?.length ? (
                        <p className="text-xs text-zinc-500">No persistent volumes attached.</p>
                      ) : (
                        <div className="space-y-1.5">
                          {stack.uniqueMounts.map((m, idx) => (
                            <div
                              key={`${m.type}-${m.source}-${idx}`}
                              className="flex items-center justify-between gap-2 text-xs"
                            >
                              <span className="truncate font-mono text-zinc-200">
                                {m.type === 'volume' ? m.name || m.source : m.source}
                              </span>
                              <Badge variant="neutral">{m.type}</Badge>
                            </div>
                          ))}
                        </div>
                      )}
                    </div>
                  </div>
                </Card>
                )
              })}
            </div>
          )}
        </>
      )}

      {/* Containers Table */}
      {view === 'containers' && (
        <>
          {filteredContainers.length === 0 ? (
            <EmptyState
              icon={Container}
              title="No containers found"
              description="Make sure Docker is running and /var/run/docker.sock is accessible."
            />
          ) : (
            <Card className="overflow-hidden">
              <div className="overflow-x-auto">
                <table className="w-full text-left text-sm">
                  <thead className="border-b border-zinc-800 bg-zinc-950/70 text-xs uppercase tracking-wider text-zinc-400">
                    <tr>
                      <th className="px-4 py-3 font-medium">Container</th>
                      <th className="px-4 py-3 font-medium">State</th>
                      <th className="px-4 py-3 font-medium">Compose</th>
                      <th className="px-4 py-3 font-medium">Schedule</th>
                      <th className="px-4 py-3 font-medium">Attached Volumes / Mounts</th>
                      <th className="px-4 py-3 font-medium text-right">Actions</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-zinc-800/70">
                    {filteredContainers.map((c) => {
                      const backupableMounts = (c.mounts || []).filter((m) => m.backupable)
                      const directSchedules = scheduleListFor(
                        schedulesByType.container,
                        c.id,
                        c.name,
                      )
                      const stackSchedules = c.composeProject
                        ? scheduleListFor(schedulesByType.stack, c.composeProject)
                        : []
                      return (
                        <tr key={c.id} className="hover:bg-zinc-900/50 transition-colors">
                          <td className="px-4 py-3.5 align-top">
                            <div className="flex flex-wrap items-center gap-1.5">
                              <span className="font-medium text-zinc-100">{c.name}</span>
                              {c.isSelf && <Badge variant="warning">Self</Badge>}
                              {c.excluded && <Badge variant="neutral">Excluded</Badge>}
                              {c.detectedDb && (
                                <Badge variant="success">
                                  <Database className="h-3 w-3" />
                                  {c.detectedDb}
                                </Badge>
                              )}
                              {c.gitopsSchedule && (
                                <Badge variant="info" className="font-mono text-[10px]">
                                  <CalendarClock className="h-2.5 w-2.5" />
                                  {c.gitopsSchedule}
                                </Badge>
                              )}
                              {(c.preHook || c.postHook) && (
                                <Badge variant="info">
                                  <Terminal className="h-3 w-3" />
                                  Exec Hook
                                </Badge>
                              )}
                            </div>
                            <div className="mt-0.5 font-mono text-xs text-zinc-400">
                              {c.image} · {c.shortID || c.shortId}
                            </div>
                            {c.lastBackupAt && (
                              <div className="mt-1 text-[11px] text-zinc-500">
                                Last backup: {formatRelativeTime(c.lastBackupAt)} ({c.lastBackupStatus})
                              </div>
                            )}
                            {c.lastRestore?.isActiveRollback && (
                              <div className="mt-1 inline-flex items-center gap-1 text-[11px] text-sky-400">
                                <RotateCcw className="h-2.5 w-2.5 shrink-0" />
                                <span>
                                  Rolled back to #{c.lastRestore.backupLogId} ({formatDateTime(c.lastRestore.backupStartedAt)})
                                </span>
                              </div>
                            )}
                          </td>

                          <td className="px-4 py-3.5 align-top">
                            <Badge variant={stateVariant(c.state)}>{c.state}</Badge>
                            <div className="mt-1 text-[11px] text-zinc-500">{c.status}</div>
                          </td>

                          <td className="px-4 py-3.5 align-top">
                            {c.composeProject ? (
                              <div>
                                <Badge variant="info">{c.composeProject}</Badge>
                                {c.composeService && (
                                  <div className="mt-1 text-xs text-zinc-400">
                                    svc: {c.composeService}
                                  </div>
                                )}
                              </div>
                            ) : (
                              <span className="text-xs text-zinc-500">Standalone</span>
                            )}
                          </td>

                          <td className="px-4 py-3.5 align-top">
                            {directSchedules.length > 0 ? (
                              <div className="flex flex-col items-start gap-1">
                                {directSchedules.map((sched) => (
                                  <SchedulePill
                                    key={sched.id}
                                    schedule={sched}
                                    targetName={c.name}
                                    onEdit={(s) => onOpenSchedule(null, s)}
                                  />
                                ))}
                              </div>
                            ) : stackSchedules.length > 0 ? (
                              <div className="flex flex-col items-start gap-1">
                                {stackSchedules.map((sched) => (
                                  <SchedulePill
                                    key={sched.id}
                                    schedule={sched}
                                    viaStack={c.composeProject}
                                    onEdit={(s) => onOpenSchedule(null, s)}
                                  />
                                ))}
                              </div>
                            ) : c.excluded || c.isSelf ? (
                              <span className="text-xs text-zinc-500">—</span>
                            ) : (
                              <Badge variant="warning">No schedule</Badge>
                            )}
                          </td>

                          <td className="px-4 py-3.5 align-top">
                            {c.mounts?.length ? (
                              <div className="flex flex-wrap gap-1.5 max-w-md">
                                {c.mounts.map((m, idx) => (
                                  <span
                                    key={`${m.destination}-${idx}`}
                                    title={`${m.source} -> ${m.destination}`}
                                    className={`inline-flex items-center gap-1 rounded-md border px-2 py-0.5 font-mono text-[11px] ${
                                      m.isDockerSocket
                                        ? 'border-amber-500/30 bg-amber-500/10 text-amber-300'
                                        : m.backupable
                                          ? 'border-zinc-700 bg-zinc-950 text-zinc-200'
                                          : 'border-zinc-800 bg-zinc-900 text-zinc-500'
                                    }`}
                                  >
                                    {m.isDockerSocket ? (
                                      <ShieldAlert className="h-3 w-3 shrink-0" />
                                    ) : (
                                      <HardDrive className="h-3 w-3 shrink-0 text-sky-400" />
                                    )}
                                    <span className="truncate max-w-[180px]">
                                      {m.type === 'volume' ? m.name || m.destination : m.destination}
                                    </span>
                                  </span>
                                ))}
                              </div>
                            ) : (
                              <span className="text-xs text-zinc-500">No mounts</span>
                            )}
                          </td>

                          <td className="px-4 py-3.5 align-top text-right">
                            <div className="inline-flex items-center gap-2">
                              <Button
                                variant="outline"
                                size="sm"
                                disabled={backupableMounts.length === 0}
                                onClick={() =>
                                  onOpenSchedule({
                                    targetType: 'container',
                                    targetId: c.name,
                                    targetName: c.name,
                                    detectedDb: c.detectedDb,
                                    gitopsSchedule: c.gitopsSchedule,
                                    gitopsExcludePaths: c.gitopsExcludePaths,
                                    gitopsCompression: c.gitopsCompression,
                                    gitopsDbHotDump: c.gitopsDbHotDump,
                                  })
                                }
                              >
                                <CalendarPlus className="h-3.5 w-3.5" />
                                Schedule
                              </Button>
                              <Button
                                size="sm"
                                disabled={backupableMounts.length === 0}
                                onClick={() =>
                                  onOpenBackup({
                                    targetType: 'container',
                                    targetId: c.name,
                                    targetName: c.name,
                                    mounts: backupableMounts,
                                    detectedDb: c.detectedDb,
                                    gitopsExcludePaths: c.gitopsExcludePaths,
                                    gitopsCompression: c.gitopsCompression,
                                    gitopsDbHotDump: c.gitopsDbHotDump,
                                  })
                                }
                              >
                                <Archive className="h-3.5 w-3.5" />
                                Run Backup
                              </Button>
                            </div>
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            </Card>
          )}
        </>
      )}

      {/* Standalone / Orphan Volumes View */}
      {view === 'volumes' && (
        <>
          {filteredVolumes.length === 0 ? (
            <EmptyState
              icon={HardDrive}
              title="No Docker volumes found"
              description="Named Docker volumes appear here, highlighting unattached orphan volumes."
            />
          ) : (
            <Card className="overflow-hidden">
              <div className="overflow-x-auto">
                <table className="w-full text-left text-sm">
                  <thead className="border-b border-zinc-800 bg-zinc-950/70 text-xs uppercase tracking-wider text-zinc-400">
                    <tr>
                      <th className="px-4 py-3 font-medium">Volume Name</th>
                      <th className="px-4 py-3 font-medium">Status</th>
                      <th className="px-4 py-3 font-medium">Attached Containers</th>
                      <th className="px-4 py-3 font-medium">Driver</th>
                      <th className="px-4 py-3 font-medium text-right">Actions</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-zinc-800/70">
                    {filteredVolumes.map((v) => {
                      const volumeSchedules = scheduleListFor(schedulesByType.volume, v.name)
                      return (
                      <tr key={v.name} className="hover:bg-zinc-900/50 transition-colors">
                        <td className="px-4 py-3.5 align-top">
                          <div className="font-mono text-xs font-medium text-zinc-100">{v.name}</div>
                          {volumeSchedules.length > 0 && (
                            <div className="mt-1.5 flex flex-wrap gap-1">
                              {volumeSchedules.map((sched) => (
                                <SchedulePill
                                  key={sched.id}
                                  schedule={sched}
                                  onEdit={(s) => onOpenSchedule(null, s)}
                                />
                              ))}
                            </div>
                          )}
                          {v.lastBackupAt && (
                            <div className="mt-1 text-[11px] text-zinc-500">
                              Last backup: {formatRelativeTime(v.lastBackupAt)} ({v.lastBackupStatus})
                            </div>
                          )}
                        </td>
                        <td className="px-4 py-3.5 align-top">
                          {v.inUse ? (
                            <Badge variant="success">Attached</Badge>
                          ) : (
                            <Badge variant="warning">Orphan / Unattached</Badge>
                          )}
                        </td>
                        <td className="px-4 py-3.5 align-top">
                          {v.attachedContainers?.length ? (
                            <div className="flex flex-wrap gap-1">
                              {v.attachedContainers.map((ctr) => (
                                <Badge key={ctr} variant="neutral">
                                  {ctr}
                                </Badge>
                              ))}
                            </div>
                          ) : (
                            <span className="text-xs text-zinc-500">None</span>
                          )}
                        </td>
                        <td className="px-4 py-3.5 align-top font-mono text-xs text-zinc-400">
                          {v.driver}
                        </td>
                        <td className="px-4 py-3.5 align-top text-right">
                          <div className="inline-flex items-center gap-2">
                            <Button
                              variant="outline"
                              size="sm"
                              onClick={() =>
                                onOpenSchedule({
                                  targetType: 'volume',
                                  targetId: v.name,
                                  targetName: v.name,
                                })
                              }
                            >
                              <CalendarPlus className="h-3.5 w-3.5" />
                              Schedule
                            </Button>
                            <Button
                              size="sm"
                              onClick={() =>
                                onOpenBackup({
                                  targetType: 'volume',
                                  targetId: v.name,
                                  targetName: v.name,
                                  mounts: [
                                    {
                                      type: 'volume',
                                      name: v.name,
                                      source: v.mountpoint,
                                      destination: '/volume',
                                      backupable: true,
                                    },
                                  ],
                                })
                              }
                            >
                              <Archive className="h-3.5 w-3.5" />
                              Backup
                            </Button>
                            {!v.inUse && (
                              <Button
                                variant="danger"
                                size="sm"
                                title="Delete unattached volume"
                                onClick={() => handleDeleteVolume(v)}
                              >
                                <Trash2 className="h-3.5 w-3.5" />
                              </Button>
                            )}
                          </div>
                        </td>
                      </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            </Card>
          )}
        </>
      )}
    </div>
  )
}
