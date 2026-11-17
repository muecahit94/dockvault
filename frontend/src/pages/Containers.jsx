import { useMemo, useState } from 'react'
import {
  Archive,
  CalendarPlus,
  Container,
  HardDrive,
  Layers,
  RefreshCw,
  Search,
  ShieldAlert,
  Terminal,
  Trash2,
} from 'lucide-react'
import { api, formatRelativeTime } from '../api/client'
import { Badge, Button, Card, EmptyState, Input, Switch } from '../components/ui'

function stateVariant(state) {
  if (state === 'running') return 'success'
  if (state === 'paused') return 'warning'
  return 'neutral'
}

export function Containers({
  containers,
  stacks,
  volumes = [],
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
              {filteredStacks.map((stack) => (
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
                        {stack.lastBackupAt ? (
                          <Badge variant={stack.lastBackupStatus === 'failed' ? 'danger' : 'neutral'}>
                            Last backup {formatRelativeTime(stack.lastBackupAt)}
                          </Badge>
                        ) : (
                          <Badge variant="warning">Never backed up</Badge>
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
                        {stack.containers.map((c) => (
                          <div
                            key={c.id}
                            className="flex items-center justify-between gap-2 text-xs"
                          >
                            <div className="flex items-center gap-2 min-w-0">
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
                            </div>
                            <Badge variant={stateVariant(c.state)}>{c.state}</Badge>
                          </div>
                        ))}
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
              ))}
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
                      <th className="px-4 py-3 font-medium">Attached Volumes / Mounts</th>
                      <th className="px-4 py-3 font-medium text-right">Actions</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-zinc-800/70">
                    {filteredContainers.map((c) => {
                      const backupableMounts = (c.mounts || []).filter((m) => m.backupable)
                      return (
                        <tr key={c.id} className="hover:bg-zinc-900/50 transition-colors">
                          <td className="px-4 py-3.5 align-top">
                            <div className="flex flex-wrap items-center gap-1.5">
                              <span className="font-medium text-zinc-100">{c.name}</span>
                              {c.isSelf && <Badge variant="warning">Self</Badge>}
                              {c.excluded && <Badge variant="neutral">Excluded</Badge>}
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
                    {filteredVolumes.map((v) => (
                      <tr key={v.name} className="hover:bg-zinc-900/50 transition-colors">
                        <td className="px-4 py-3.5 align-top">
                          <div className="font-mono text-xs font-medium text-zinc-100">{v.name}</div>
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
                    ))}
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
