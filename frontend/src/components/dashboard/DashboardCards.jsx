import {
  Archive,
  Bell,
  BellOff,
  CheckCircle2,
  HardDrive,
  Loader2,
  RotateCcw,
  ShieldCheck,
  XCircle,
} from 'lucide-react'
import {
  formatBytes,
  formatDateTime,
  formatDuration,
  formatRelativeTime,
} from '../../api/client'
import { Badge, Button, Card, EmptyState } from '../ui'

export function ActiveRollbacksBanner({ activeRollbacks = [], onViewHistory }) {
  if (activeRollbacks.length === 0) return null
  return (
    <Card className="border-sky-500/30 bg-sky-500/5 p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex items-start gap-3">
          <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl border border-sky-500/30 bg-sky-500/10 text-sky-400">
            <RotateCcw className="h-4 w-4" />
          </div>
          <div className="space-y-1">
            <div className="flex flex-wrap items-center gap-2">
              <h3 className="text-sm font-semibold text-zinc-100">
                Active Rolled-Back Targets ({activeRollbacks.length})
              </h3>
              <Badge variant="info">Restored State</Badge>
            </div>
            <div className="space-y-1 text-xs text-zinc-300">
              {activeRollbacks.map((r) => (
                <div key={r.id} className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
                  <span className="font-medium text-zinc-100">{r.targetName}</span>
                  <Badge variant="neutral">{r.targetType}</Badge>
                  <span>→ rolled back to</span>
                  <span className="font-mono text-sky-300">
                    #{r.backupLogId} ({r.backupFileName || 'backup'})
                  </span>
                  <span className="text-zinc-400">from {formatDateTime(r.backupStartedAt)}</span>
                  <span className="text-zinc-500">· restored {formatRelativeTime(r.restoredAt)}</span>
                </div>
              ))}
            </div>
          </div>
        </div>
        <Button variant="outline" size="sm" onClick={onViewHistory}>
          View Restore Log
        </Button>
      </div>
    </Card>
  )
}

export function StatusBadge({ status }) {
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

export function UpcomingSchedulesCard({ upcomingSchedules = [], onNavigate }) {
  return (
    <Card className="h-full flex flex-col p-5">
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
        <div className="space-y-2.5 overflow-y-auto max-h-80 pr-1">
          {upcomingSchedules.map((s) => (
            <div key={s.id} className="rounded-xl border border-zinc-800 bg-zinc-950/60 p-3">
              <div className="flex items-center justify-between gap-2">
                <span className="truncate text-sm font-medium text-zinc-100">{s.name}</span>
                <span className="font-mono text-[11px] text-sky-400 shrink-0">{s.cronExpr}</span>
              </div>
              <div className="mt-1 flex items-center justify-between gap-2 text-xs text-zinc-400">
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
  )
}

export function RecentBackupsCard({ recentBackups = [], onNavigate }) {
  return (
    <Card className="h-full flex flex-col p-5">
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
                <div className="flex flex-wrap items-center gap-2">
                  <span className="truncate text-sm font-medium text-zinc-100">{b.targetName}</span>
                  <Badge variant="neutral">{b.targetType}</Badge>
                  <StatusBadge status={b.status} />
                </div>
                <div className="mt-0.5 flex flex-wrap items-center gap-x-2.5 gap-y-0.5 text-xs text-zinc-400">
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
  )
}

export function RecentRestoresCard({
  recentRestores = [],
  activeRollbacks = [],
  onNavigate,
}) {
  return (
    <Card className="h-full flex flex-col p-5">
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-center gap-2">
          <RotateCcw className="h-4 w-4 text-sky-400 shrink-0" />
          <div>
            <h3 className="text-sm font-semibold text-zinc-100">Recent Restores &amp; Rollbacks</h3>
            <p className="text-xs text-zinc-400">
              Snapshot version restored or rolled back to per target
            </p>
          </div>
        </div>
        <Button variant="ghost" size="sm" onClick={() => onNavigate('history')}>
          History
        </Button>
      </div>

      {recentRestores.length === 0 ? (
        <EmptyState
          icon={RotateCcw}
          title="No restores recorded yet"
          description="When you restore a backup, the target and exact backup version rolled back to will appear here."
        />
      ) : (
        <div className="divide-y divide-zinc-800/80">
          {recentRestores.map((r) => {
            const isActiveVersion =
              r.status === 'success' &&
              r.mode === 'inplace' &&
              activeRollbacks.some((a) => a.id === r.id)
            return (
              <button
                key={r.id}
                type="button"
                onClick={() => onNavigate('history')}
                className="w-full text-left flex items-center justify-between gap-3 py-3 px-2 -mx-2 rounded-lg hover:bg-zinc-900/50 transition-colors cursor-pointer"
              >
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="truncate text-sm font-medium text-zinc-100">
                      {r.targetName}
                    </span>
                    <Badge variant="neutral">{r.targetType}</Badge>
                    <StatusBadge status={r.status} />
                    {r.mode === 'staging' ? (
                      <Badge variant="neutral">Staging: {r.targetVolume}</Badge>
                    ) : (
                      <Badge variant={isActiveVersion ? 'info' : 'neutral'}>
                        {isActiveVersion ? 'Current rollback' : 'In-place'}
                      </Badge>
                    )}
                    {r.restoredPaths > 0 && (
                      <Badge variant="neutral">{r.restoredPaths} path(s)</Badge>
                    )}
                  </div>
                  <div className="mt-1 flex flex-wrap items-center gap-x-2.5 gap-y-0.5 text-xs text-zinc-400">
                    <span className="text-zinc-300">
                      Version: <span className="font-mono text-sky-400">#{r.backupLogId}</span>{' '}
                      {r.backupFileName || 'archive'}
                    </span>
                    <span>·</span>
                    <span>backed up {formatDateTime(r.backupStartedAt)}</span>
                    <span>·</span>
                    <span>restored {formatDateTime(r.restoredAt)}</span>
                    <span>·</span>
                    <span>{formatDuration(r.durationMs)}</span>
                    {r.preRestoreBackupId && (
                      <>
                        <span>·</span>
                        <span className="text-zinc-300">
                          safety snapshot #{r.preRestoreBackupId}
                        </span>
                      </>
                    )}
                  </div>
                  {r.errorMessage && (
                    <p className="mt-1 truncate text-xs text-rose-400">{r.errorMessage}</p>
                  )}
                </div>
              </button>
            )
          })}
        </div>
      )}
    </Card>
  )
}

export function InfrastructureStrip({
  providers = [],
  securityOverview = {},
  encPct = 0,
  notifOverview,
  onNavigate,
}) {
  return (
    <div className="grid grid-cols-1 md:grid-cols-3 gap-6 items-stretch">
      <Card className="h-full flex flex-col p-5">
        <div className="flex items-center justify-between mb-3">
          <div>
            <h3 className="text-sm font-semibold text-zinc-100">Storage Destinations</h3>
            <p className="text-xs text-zinc-400">{providers.length} configured provider(s)</p>
          </div>
          <Button variant="ghost" size="sm" onClick={() => onNavigate('settings')}>
            Configure
          </Button>
        </div>
        <div className="space-y-2 flex-1">
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

      <Card className="h-full flex flex-col p-5">
        <div className="flex items-center justify-between mb-3">
          <div className="flex items-center gap-2">
            <ShieldCheck className="h-4 w-4 text-emerald-400 shrink-0" />
            <div>
              <h3 className="text-sm font-semibold text-zinc-100">
                Encryption &amp; Security Health
              </h3>
              <p className="text-xs text-zinc-400">Access control &amp; archive protection</p>
            </div>
          </div>
          <Button variant="ghost" size="sm" onClick={() => onNavigate('settings', 'security')}>
            Review
          </Button>
        </div>
        <div className="rounded-xl border border-zinc-800 bg-zinc-950/60 p-3 space-y-2 text-xs flex-1">
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
            <Badge variant={(securityOverview.encryptedProviders || 0) > 0 ? 'info' : 'neutral'}>
              {securityOverview.encryptedProviders ?? 0} / {securityOverview.totalProviders ?? 0}
            </Badge>
          </div>
          <div className="flex items-center justify-between">
            <span className="text-zinc-400">Encrypted Snapshots</span>
            <span className="font-mono text-zinc-200 tabular-nums">
              {securityOverview.encryptedBackups ?? 0} /{' '}
              {securityOverview.totalSuccessfulBackups ?? 0} ({encPct}%)
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

      <Card className="h-full flex flex-col p-5">
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
          <div className="rounded-xl border border-zinc-800 bg-zinc-950/60 p-3 text-xs space-y-1.5 flex-1">
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
          <div className="space-y-2 flex-1">
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
                <div className="text-[11px] text-zinc-400">
                  Triggers: {ch.summary.triggers}
                  {ch.summary.detail ? ` · ${ch.summary.detail}` : ''}
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>
    </div>
  )
}
