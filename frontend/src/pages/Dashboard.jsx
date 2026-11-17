import {
  Activity,
  Archive,
  Bell,
  BellOff,
  CalendarClock,
  CheckCircle2,
  Container,
  Database,
  HardDrive,
  Layers,
  Loader2,
  XCircle,
} from 'lucide-react'
import {
  formatBytes,
  formatDateTime,
  formatDuration,
  formatRelativeTime,
  summarizeNotificationsList,
} from '../api/client'
import { Badge, Button, Card, EmptyState } from '../components/ui'

function StatusBadge({ status }) {
  if (status === 'success') {
    return (
      <Badge variant="success">
        <CheckCircle2 className="h-3 w-3" />
        Success
      </Badge>
    )
  }
  if (status === 'failed') {
    return (
      <Badge variant="danger">
        <XCircle className="h-3 w-3" />
        Failed
      </Badge>
    )
  }
  return (
    <Badge variant="info">
      <Loader2 className="h-3 w-3 animate-spin" />
      Running
    </Badge>
  )
}

export function Dashboard({ dashboard, providers = [], notifications = [], onNavigate }) {
  const notifOverview = summarizeNotificationsList(notifications)
  const stats = dashboard?.stats || {}
  const docker = dashboard?.docker || {}
  const recentBackups = dashboard?.recentBackups || []
  const upcomingSchedules = dashboard?.upcomingSchedules || []
  const dailyActivity = dashboard?.dailyActivity || []
  const maxDailyRuns = Math.max(
    1,
    ...dailyActivity.map((d) => (d.success || 0) + (d.failed || 0)),
  )

  const statCards = [
    {
      label: 'Containers',
      value: `${stats.containersRunning ?? 0} / ${stats.containersTotal ?? 0}`,
      hint: 'Running / Total discovered',
      icon: Container,
      color: 'text-sky-400',
    },
    {
      label: 'Compose Stacks',
      value: stats.stacksTotal ?? 0,
      hint: 'Grouped by project label',
      icon: Layers,
      color: 'text-violet-400',
    },
    {
      label: 'Active Schedules',
      value: stats.activeSchedules ?? 0,
      hint: 'Automated cron jobs',
      icon: CalendarClock,
      color: 'text-emerald-400',
    },
    {
      label: 'Backup Storage',
      value: formatBytes(stats.totalBackupBytes ?? 0),
      hint: `${stats.successfulBackups ?? 0} succeeded · ${stats.failedBackups ?? 0} failed`,
      icon: Database,
      color: 'text-amber-400',
    },
  ]

  return (
    <div className="space-y-6">
      {/* Docker connection banner */}
      <Card className="p-4">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex items-center gap-3">
            <div
              className={`flex h-10 w-10 items-center justify-center rounded-xl border ${
                docker.connected
                  ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-400'
                  : 'border-rose-500/30 bg-rose-500/10 text-rose-400'
              }`}
            >
              <Activity className="h-5 w-5" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h2 className="text-sm font-semibold text-zinc-100">Docker Engine</h2>
                <Badge variant={docker.connected ? 'success' : 'danger'}>
                  {docker.connected ? 'Connected' : 'Disconnected'}
                </Badge>
              </div>
              <p className="mt-0.5 text-xs text-zinc-400">
                {docker.connected
                  ? `Engine v${docker.serverVersion} · ${docker.os} (${docker.architecture}) · Helper: ${dashboard?.helperImage}`
                  : docker.error || 'Mount /var/run/docker.sock to discover containers and volumes.'}
              </p>
            </div>
          </div>

          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" onClick={() => onNavigate('containers')}>
              <Container className="h-3.5 w-3.5" />
              Browse Containers
            </Button>
            <Button size="sm" onClick={() => onNavigate('schedules')}>
              <CalendarClock className="h-3.5 w-3.5" />
              Manage Schedules
            </Button>
          </div>
        </div>
      </Card>

      {/* KPI cards */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        {statCards.map((item) => {
          const Icon = item.icon
          return (
            <Card key={item.label} className="p-4">
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium text-zinc-400">{item.label}</span>
                <Icon className={`h-4 w-4 ${item.color}`} />
              </div>
              <div className="mt-2 text-2xl font-semibold tracking-tight text-zinc-100">
                {item.value}
              </div>
              <p className="mt-1 text-xs text-zinc-500">{item.hint}</p>
            </Card>
          )
        })}
      </div>

      {/* Two-column activity and upcoming */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <div className="lg:col-span-2 space-y-6">
          {dailyActivity.length > 0 && (
            <Card className="p-5">
              <div className="flex items-center justify-between mb-4">
                <div>
                  <h3 className="text-sm font-semibold text-zinc-100">7-Day Backup Activity</h3>
                  <p className="text-xs text-zinc-400">Daily successful and failed archive runs</p>
                </div>
                <div className="flex items-center gap-3 text-[11px] text-zinc-400">
                  <span className="flex items-center gap-1.5">
                    <span className="h-2 w-2 rounded-full bg-emerald-400" /> Success
                  </span>
                  <span className="flex items-center gap-1.5">
                    <span className="h-2 w-2 rounded-full bg-rose-400" /> Failed
                  </span>
                </div>
              </div>
              <div className="grid grid-cols-7 gap-2 items-end h-28 pt-2">
                {dailyActivity.map((day) => {
                  const total = (day.success || 0) + (day.failed || 0)
                  const successH = Math.round(((day.success || 0) / maxDailyRuns) * 100)
                  const failedH = Math.round(((day.failed || 0) / maxDailyRuns) * 100)
                  const label = day.date.slice(5)
                  return (
                    <div
                      key={day.date}
                      title={`${day.date}: ${day.success} succeeded, ${day.failed} failed (${formatBytes(day.bytes)})`}
                      className="flex flex-col items-center gap-1.5 h-full justify-end"
                    >
                      <div className="text-[10px] text-zinc-500 h-3.5">
                        {total > 0 ? total : ''}
                      </div>
                      <div className="w-full max-w-[34px] flex-1 rounded-md bg-zinc-950/80 border border-zinc-800/70 p-0.5 flex flex-col justify-end gap-0.5 overflow-hidden">
                        {day.failed > 0 && (
                          <div
                            style={{ height: `${Math.max(failedH, 12)}%` }}
                            className="w-full rounded-xs bg-rose-500/80"
                          />
                        )}
                        {day.success > 0 && (
                          <div
                            style={{ height: `${Math.max(successH, 12)}%` }}
                            className="w-full rounded-xs bg-emerald-500/80"
                          />
                        )}
                      </div>
                      <div className="font-mono text-[10px] text-zinc-400">{label}</div>
                    </div>
                  )
                })}
              </div>
            </Card>
          )}

          <Card className="p-5">
            <div className="flex items-center justify-between mb-4">
              <div>
                <h3 className="text-sm font-semibold text-zinc-100">Recent Backup Activity</h3>
                <p className="text-xs text-zinc-400">Latest manual and scheduled archive runs</p>
              </div>
              <Button variant="ghost" size="sm" onClick={() => onNavigate('history')}>
                View all
              </Button>
            </div>

            {recentBackups.length === 0 ? (
              <EmptyState
                icon={Archive}
                title="No backups yet"
                description="Head over to Containers & Stacks to run your first safe volume backup."
                action={
                  <Button size="sm" onClick={() => onNavigate('containers')}>
                    Run First Backup
                  </Button>
                }
              />
            ) : (
              <div className="divide-y divide-zinc-800/80">
                {recentBackups.map((b) => (
                  <button
                    key={b.id}
                    type="button"
                    onClick={() => onNavigate('history')}
                    className="w-full text-left flex items-center justify-between gap-3 py-3 px-2 -mx-2 rounded-lg hover:bg-zinc-900/50 transition-colors cursor-pointer"
                  >
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="truncate text-sm font-medium text-zinc-100">
                          {b.targetName}
                        </span>
                        <Badge variant="neutral">{b.targetType}</Badge>
                        <StatusBadge status={b.status} />
                      </div>
                      <div className="mt-0.5 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-zinc-400">
                        <span>{b.storageProviderName}</span>
                        <span>·</span>
                        <span>{formatDateTime(b.startedAt)}</span>
                        {b.status === 'success' && (
                          <>
                            <span>·</span>
                            <span>{formatBytes(b.sizeBytes)}</span>
                            <span>·</span>
                            <span>{formatDuration(b.durationMs)}</span>
                          </>
                        )}
                      </div>
                      {b.errorMessage && (
                        <p className="mt-1 truncate text-xs text-rose-400">{b.errorMessage}</p>
                      )}
                    </div>
                  </button>
                ))}
              </div>
            )}
          </Card>
        </div>

        <div className="space-y-6">
          <Card className="p-5">
            <div className="flex items-center justify-between mb-4">
              <div>
                <h3 className="text-sm font-semibold text-zinc-100">Upcoming Schedules</h3>
                <p className="text-xs text-zinc-400">Active automated jobs</p>
              </div>
              <Button variant="ghost" size="sm" onClick={() => onNavigate('schedules')}>
                Manage
              </Button>
            </div>

            {upcomingSchedules.length === 0 ? (
              <EmptyState
                icon={HardDrive}
                title="No active schedules"
                description="Create a cron schedule to automate backups."
              />
            ) : (
              <div className="space-y-3">
                {upcomingSchedules.map((s) => (
                  <div
                    key={s.id}
                    className="rounded-xl border border-zinc-800 bg-zinc-950/60 p-3"
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span className="truncate text-sm font-medium text-zinc-100">{s.name}</span>
                      <span className="font-mono text-[11px] text-sky-400">{s.cronExpr}</span>
                    </div>
                    <div className="mt-1 flex items-center justify-between text-xs text-zinc-400">
                      <span className="truncate">
                        {s.targetName} → {s.storageTitle || 'Default'}
                      </span>
                      <span className="shrink-0 text-zinc-300">{formatRelativeTime(s.nextRunAt)}</span>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </Card>

          <Card className="p-5">
            <div className="flex items-center justify-between mb-3">
              <div>
                <h3 className="text-sm font-semibold text-zinc-100">Storage Destinations</h3>
                <p className="text-xs text-zinc-400">{providers.length} configured provider(s)</p>
              </div>
              <Button variant="ghost" size="sm" onClick={() => onNavigate('settings')}>
                Configure
              </Button>
            </div>
            <div className="space-y-2">
              {providers.map((p) => (
                <div
                  key={p.id}
                  className="flex items-center justify-between rounded-xl border border-zinc-800 bg-zinc-950/60 px-3 py-2 text-xs"
                >
                  <span className="truncate font-medium text-zinc-200">{p.name}</span>
                  <div className="flex items-center gap-1.5 shrink-0">
                    <Badge variant="neutral">{p.type.toUpperCase()}</Badge>
                    {p.isDefault && <Badge variant="success">Default</Badge>}
                  </div>
                </div>
              ))}
            </div>
          </Card>

          <Card className="p-5">
            <div className="flex items-center justify-between mb-3">
              <div>
                <h3 className="text-sm font-semibold text-zinc-100">Alert Notifications</h3>
                <p className="text-xs text-zinc-400">
                  {notifOverview.configuredCount > 0
                    ? `${notifOverview.activeCount} active · ${notifOverview.configuredCount} configured`
                    : 'Backup status & failure alerts'}
                </p>
              </div>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => onNavigate('settings', 'notifications')}
              >
                {notifOverview.configuredCount > 0 ? 'Manage' : 'Configure'}
              </Button>
            </div>
            {notifOverview.configuredCount === 0 ? (
              <div className="rounded-xl border border-zinc-800 bg-zinc-950/60 p-3 text-xs space-y-1.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="flex items-center gap-2 font-medium text-zinc-200">
                    <BellOff className="h-3.5 w-3.5 text-zinc-400 shrink-0" />
                    No channels set
                  </span>
                  <Badge variant="neutral">Not configured</Badge>
                </div>
                <p className="text-[11px] text-zinc-400">
                  Set up Telegram, Discord, Slack, ntfy, or webhooks to receive backup alerts.
                </p>
              </div>
            ) : (
              <div className="space-y-2">
                {notifOverview.channels.map((ch) => (
                  <div
                    key={ch.id}
                    className="rounded-xl border border-zinc-800 bg-zinc-950/60 px-3 py-2 text-xs space-y-1"
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span className="flex items-center gap-1.5 min-w-0 font-medium text-zinc-200">
                        {ch.summary.active ? (
                          <Bell className="h-3.5 w-3.5 text-emerald-400 shrink-0" />
                        ) : (
                          <BellOff className="h-3.5 w-3.5 text-zinc-400 shrink-0" />
                        )}
                        <span className="truncate">{ch.displayName}</span>
                      </span>
                      <div className="flex items-center gap-1.5 shrink-0">
                        <Badge variant="neutral" className="text-[10px]">
                          {ch.summary.providerLabel}
                        </Badge>
                        <Badge variant={ch.summary.active ? 'success' : 'warning'}>
                          {ch.summary.active ? 'Active' : 'Paused'}
                        </Badge>
                      </div>
                    </div>
                    <div className="flex items-center justify-between gap-2 text-[11px] text-zinc-400">
                      <span className="truncate font-mono">{ch.summary.targetSummary}</span>
                      <span className="shrink-0">{ch.summary.triggers}</span>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </Card>
        </div>
      </div>
    </div>
  )
}
