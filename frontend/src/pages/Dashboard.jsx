import { useState } from 'react'
import {
  Activity,
  Archive,
  BarChart3,
  Bell,
  BellOff,
  CalendarClock,
  CheckCircle2,
  Container,
  Database,
  HardDrive,
  Layers,
  Loader2,
  Lock,
  ShieldCheck,
  XCircle,
} from 'lucide-react'
import {
  formatBytes,
  formatDateTime,
  formatDuration,
  formatRelativeTime,
  summarizeNotificationsList,
} from '../api/client'
import { SetupChecklist } from '../components/SetupChecklist'
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
  const [activityRange, setActivityRange] = useState('7d')
  const notifOverview = summarizeNotificationsList(notifications)
  const stats = dashboard?.stats || {}
  const docker = dashboard?.docker || {}
  const recentBackups = dashboard?.recentBackups || []
  const upcomingSchedules = dashboard?.upcomingSchedules || []
  const dailyActivity = dashboard?.dailyActivity || []
  const storageAnalytics = dashboard?.storageAnalytics || {}
  const securityOverview = dashboard?.securityOverview || {}

  const windowCount = activityRange === '90d' ? 90 : activityRange === '30d' ? 30 : 7
  const visibleActivity = dailyActivity.slice(-windowCount)
  const maxDailyRuns = Math.max(
    1,
    ...visibleActivity.map((d) => (d.success || 0) + (d.failed || 0)),
  )
  const periodTotals = visibleActivity.reduce(
    (acc, d) => ({
      success: acc.success + (d.success || 0),
      failed: acc.failed + (d.failed || 0),
      bytes: acc.bytes + (d.bytes || 0),
    }),
    { success: 0, failed: 0, bytes: 0 },
  )
  const periodRuns = periodTotals.success + periodTotals.failed
  const periodSuccessRate =
    periodRuns > 0 ? Math.round((periodTotals.success / periodRuns) * 100) : null

  const providerBreakdown = storageAnalytics.byProvider || []
  const totalProviderBytes = Math.max(
    1,
    providerBreakdown.reduce((sum, p) => sum + (p.totalBytes || 0), 0),
  )
  const topTargets = storageAnalytics.byTarget || []
  const disk = storageAnalytics.disk
  const diskUsedPct =
    disk && disk.totalBytes > 0 ? Math.round((disk.usedBytes / disk.totalBytes) * 100) : 0
  const encPct =
    securityOverview.totalSuccessfulBackups > 0
      ? Math.round(
          ((securityOverview.encryptedBackups || 0) / securityOverview.totalSuccessfulBackups) * 100,
        )
      : 0

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

      <SetupChecklist
        dashboard={dashboard}
        providers={providers}
        activeChannelCount={notifOverview.activeCount}
        onNavigate={onNavigate}
      />

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
            <Card className="p-5 space-y-4">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div>
                  <h3 className="text-sm font-semibold text-zinc-100">
                    Backup Health &amp; Activity ({windowCount}-Day)
                  </h3>
                  <p className="text-xs text-zinc-400">
                    {periodRuns} run{periodRuns === 1 ? '' : 's'}
                    {periodSuccessRate !== null ? ` · ${periodSuccessRate}% success` : ''} ·{' '}
                    {formatBytes(periodTotals.bytes)} archived
                  </p>
                </div>
                <div className="flex items-center gap-2">
                  <div className="flex items-center rounded-lg border border-zinc-800 bg-zinc-950 p-0.5 text-[11px]">
                    {[
                      { id: '7d', label: '7d Bars' },
                      { id: '30d', label: '30d Map' },
                      { id: '90d', label: '90d Map' },
                    ].map((opt) => (
                      <button
                        key={opt.id}
                        type="button"
                        onClick={() => setActivityRange(opt.id)}
                        className={`rounded-md px-2 py-1 font-medium transition-colors cursor-pointer ${
                          activityRange === opt.id
                            ? 'bg-zinc-800 text-zinc-100'
                            : 'text-zinc-400 hover:text-zinc-200'
                        }`}
                      >
                        {opt.label}
                      </button>
                    ))}
                  </div>
                </div>
              </div>

              {activityRange === '7d' ? (
                <div className="grid grid-cols-7 gap-2 items-end h-28 pt-2">
                  {visibleActivity.map((day) => {
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
              ) : (
                <div className="space-y-2.5">
                  <div
                    className={`grid gap-1.5 ${
                      activityRange === '90d' ? 'grid-cols-15 sm:grid-cols-18' : 'grid-cols-10 sm:grid-cols-15'
                    }`}
                  >
                    {visibleActivity.map((day) => {
                      let cellClass = 'border-zinc-800/80 bg-zinc-950/70'
                      if (day.failed > 0 && day.success === 0) {
                        cellClass = 'border-rose-500/50 bg-rose-500/70'
                      } else if (day.failed > 0 && day.success > 0) {
                        cellClass = 'border-amber-500/50 bg-amber-500/70'
                      } else if (day.success >= 3) {
                        cellClass = 'border-emerald-400/60 bg-emerald-500'
                      } else if (day.success > 0) {
                        cellClass = 'border-emerald-500/40 bg-emerald-500/45'
                      }
                      return (
                        <div
                          key={day.date}
                          title={`${day.date}: ${day.success} succeeded, ${day.failed} failed (${formatBytes(day.bytes)})`}
                          className={`h-5 rounded border transition-transform hover:scale-110 ${cellClass}`}
                        />
                      )
                    })}
                  </div>
                  <div className="flex flex-wrap items-center justify-between gap-2 text-[11px] text-zinc-400">
                    <span>
                      {visibleActivity[0]?.date} → {visibleActivity[visibleActivity.length - 1]?.date}
                    </span>
                    <div className="flex items-center gap-3">
                      <span className="flex items-center gap-1">
                        <span className="h-2.5 w-2.5 rounded-xs border border-zinc-800 bg-zinc-950" /> Idle
                      </span>
                      <span className="flex items-center gap-1">
                        <span className="h-2.5 w-2.5 rounded-xs bg-emerald-500/60" /> Healthy
                      </span>
                      <span className="flex items-center gap-1">
                        <span className="h-2.5 w-2.5 rounded-xs bg-amber-500/80" /> Mixed
                      </span>
                      <span className="flex items-center gap-1">
                        <span className="h-2.5 w-2.5 rounded-xs bg-rose-500/80" /> Failed
                      </span>
                    </div>
                  </div>
                </div>
              )}
            </Card>
          )}

          {/* Storage Analytics & Growth */}
          <Card className="p-5 space-y-4">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div className="flex items-center gap-2">
                <BarChart3 className="h-4 w-4 text-sky-400" />
                <div>
                  <h3 className="text-sm font-semibold text-zinc-100">Storage Analytics &amp; Footprint</h3>
                  <p className="text-xs text-zinc-400">
                    Host disk headroom, storage distribution, and top backup consumers
                  </p>
                </div>
              </div>
              {storageAnalytics.compression && (
                <div className="flex items-center gap-1.5 text-[11px]">
                  <Badge variant="neutral">gzip: {storageAnalytics.compression.gzip ?? 0}</Badge>
                  <Badge variant="info">zstd: {storageAnalytics.compression.zstd ?? 0}</Badge>
                </div>
              )}
            </div>

            {disk && disk.totalBytes > 0 && (
              <div className="rounded-xl border border-zinc-800 bg-zinc-950/60 p-3 space-y-2">
                <div className="flex flex-wrap items-center justify-between gap-2 text-xs">
                  <span className="text-zinc-300 font-medium">
                    Backup Volume Host Disk (<span className="font-mono text-zinc-400">{disk.path}</span>)
                  </span>
                  <span className="font-mono text-zinc-200">
                    {formatBytes(disk.freeBytes)} free · {diskUsedPct}% used of {formatBytes(disk.totalBytes)}
                  </span>
                </div>
                <div className="h-2 w-full overflow-hidden rounded-full bg-zinc-900">
                  <div
                    style={{ width: `${Math.min(diskUsedPct, 100)}%` }}
                    className={`h-full rounded-full ${
                      diskUsedPct >= 90
                        ? 'bg-rose-500'
                        : diskUsedPct >= 75
                          ? 'bg-amber-500'
                          : 'bg-sky-500'
                    }`}
                  />
                </div>
              </div>
            )}

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div className="rounded-xl border border-zinc-800 bg-zinc-950/60 p-3 space-y-2.5">
                <div className="text-[11px] font-medium uppercase tracking-wider text-zinc-400">
                  By Storage Destination
                </div>
                {providerBreakdown.length === 0 ? (
                  <p className="text-xs text-zinc-500">No storage providers configured.</p>
                ) : (
                  providerBreakdown.map((p) => {
                    const pct = Math.round(((p.totalBytes || 0) / totalProviderBytes) * 100)
                    return (
                      <div key={p.providerId} className="space-y-1 text-xs">
                        <div className="flex items-center justify-between gap-2">
                          <span className="flex items-center gap-1.5 min-w-0 font-medium text-zinc-200">
                            <span className="truncate">{p.name}</span>
                            {p.encrypted && <Lock className="h-3 w-3 text-sky-400 shrink-0" />}
                          </span>
                          <span className="shrink-0 font-mono text-[11px] text-zinc-400">
                            {formatBytes(p.totalBytes)} ({p.backupCount})
                          </span>
                        </div>
                        <div className="h-1.5 w-full overflow-hidden rounded-full bg-zinc-900">
                          <div
                            style={{ width: `${Math.max(pct, p.totalBytes > 0 ? 4 : 0)}%` }}
                            className="h-full rounded-full bg-sky-500/80"
                          />
                        </div>
                      </div>
                    )
                  })
                )}
              </div>

              <div className="rounded-xl border border-zinc-800 bg-zinc-950/60 p-3 space-y-2">
                <div className="text-[11px] font-medium uppercase tracking-wider text-zinc-400">
                  Top Space Consumers
                </div>
                {topTargets.length === 0 ? (
                  <p className="text-xs text-zinc-500">Run backups to track size per stack or volume.</p>
                ) : (
                  <div className="space-y-1.5 text-xs">
                    {topTargets.slice(0, 5).map((t) => (
                      <div
                        key={`${t.targetType}-${t.targetName}`}
                        className="flex items-center justify-between gap-2"
                      >
                        <span className="flex items-center gap-1.5 min-w-0">
                          <Badge variant="neutral" className="text-[10px] py-0 px-1.5">
                            {t.targetType}
                          </Badge>
                          <span className="truncate text-zinc-200">{t.targetName}</span>
                        </span>
                        <span className="shrink-0 font-mono text-[11px] text-zinc-400">
                          {formatBytes(t.totalBytes)} ({t.backupCount}×)
                        </span>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
          </Card>

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
              <div className="flex items-center gap-2">
                <ShieldCheck className="h-4 w-4 text-emerald-400 shrink-0" />
                <div>
                  <h3 className="text-sm font-semibold text-zinc-100">Encryption &amp; Security Health</h3>
                  <p className="text-xs text-zinc-400">Access control &amp; archive protection</p>
                </div>
              </div>
              <Button variant="ghost" size="sm" onClick={() => onNavigate('settings', 'security')}>
                Review
              </Button>
            </div>
            <div className="rounded-xl border border-zinc-800 bg-zinc-950/60 p-3 space-y-2 text-xs">
              <div className="flex items-center justify-between">
                <span className="text-zinc-400">Authentication</span>
                <Badge variant={securityOverview.authEnabled ? 'success' : 'warning'}>
                  {securityOverview.authEnabled
                    ? securityOverview.authMode === 'proxy'
                      ? 'Proxy SSO'
                      : 'Built-in Auth'
                    : 'Open (No Login)'}
                </Badge>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-zinc-400">Encrypted Destinations</span>
                <Badge
                  variant={(securityOverview.encryptedProviders || 0) > 0 ? 'info' : 'neutral'}
                >
                  {securityOverview.encryptedProviders ?? 0} / {securityOverview.totalProviders ?? 0}
                </Badge>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-zinc-400">Encrypted Snapshots</span>
                <span className="font-mono text-zinc-200">
                  {securityOverview.encryptedBackups ?? 0} / {securityOverview.totalSuccessfulBackups ?? 0} ({encPct}%)
                </span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-zinc-400">Global Fallback Key</span>
                <Badge variant={securityOverview.globalKeyConfigured ? 'success' : 'neutral'}>
                  {securityOverview.globalKeyConfigured ? 'Configured' : 'Per-Provider Only'}
                </Badge>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-zinc-400">Transport / Cookie</span>
                <Badge variant={securityOverview.httpsActive ? 'success' : 'neutral'}>
                  {securityOverview.httpsActive ? 'HTTPS Secure' : 'HTTP Local'}
                </Badge>
              </div>
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
