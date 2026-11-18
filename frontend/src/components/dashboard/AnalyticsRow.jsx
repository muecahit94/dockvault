import { useState } from 'react'
import { Activity, BarChart3, Lock } from 'lucide-react'
import { formatBytes } from '../../api/client'
import { Badge, Card, EmptyState } from '../ui'

export function BackupActivityCard({ dailyActivity = [] }) {
  const [activityRange, setActivityRange] = useState('7d')

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

  return (
    <Card className="h-full flex flex-col justify-between p-5 gap-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex items-center gap-2">
          <Activity className="h-4 w-4 text-emerald-400 shrink-0" />
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
        </div>
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

      {dailyActivity.length === 0 ? (
        <EmptyState
          icon={Activity}
          title="No activity history yet"
          description="Completed backups over the last 90 days appear here."
        />
      ) : activityRange === '7d' ? (
        <div className="flex-1 flex flex-col justify-between gap-3">
          <div className="grid grid-cols-7 gap-2.5 items-end flex-1 min-h-[164px] rounded-xl border border-zinc-800/80 bg-zinc-950/60 px-3 pt-4 pb-2.5">
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
                  <div className="font-mono text-[10px] text-zinc-400 h-3.5 leading-none tabular-nums">
                    {total > 0 ? total : '0'}
                  </div>
                  <div className="w-full max-w-[34px] flex-1 min-h-[88px] rounded-md bg-zinc-900/70 border border-zinc-800/70 p-0.5 flex flex-col justify-end gap-0.5 overflow-hidden">
                    {day.failed > 0 && (
                      <div
                        style={{ height: `${Math.max(failedH, 10)}%` }}
                        className="w-full rounded-xs bg-rose-500/85"
                      />
                    )}
                    {day.success > 0 && (
                      <div
                        style={{ height: `${Math.max(successH, 10)}%` }}
                        className="w-full rounded-xs bg-emerald-500/85"
                      />
                    )}
                  </div>
                  <div className="font-mono text-[10px] text-zinc-400 pt-0.5 border-t border-zinc-800/80 w-full text-center">
                    {label}
                  </div>
                </div>
              )
            })}
          </div>
          <div className="flex flex-wrap items-center justify-between gap-2 text-[11px] text-zinc-400">
            <span>
              {visibleActivity[0]?.date} → {visibleActivity[visibleActivity.length - 1]?.date}
            </span>
            <div className="flex items-center gap-3">
              <span className="flex items-center gap-1">
                <span className="h-2.5 w-2.5 rounded-xs bg-emerald-500/85" /> Succeeded (
                {periodTotals.success})
              </span>
              <span className="flex items-center gap-1">
                <span className="h-2.5 w-2.5 rounded-xs bg-rose-500/85" /> Failed (
                {periodTotals.failed})
              </span>
            </div>
          </div>
        </div>
      ) : (
        <div className="flex-1 flex flex-col justify-between gap-3">
          <div className="flex-1 flex items-center rounded-xl border border-zinc-800/80 bg-zinc-950/60 p-3">
            <div
              style={{
                gridTemplateColumns: `repeat(${activityRange === '90d' ? 18 : 15}, minmax(0, 1fr))`,
              }}
              className="grid gap-1.5 w-full"
            >
              {visibleActivity.map((day) => {
                let cellClass = 'border-zinc-800/80 bg-zinc-900/70'
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
          </div>
          <div className="flex flex-wrap items-center justify-between gap-2 text-[11px] text-zinc-400">
            <span>
              {visibleActivity[0]?.date} → {visibleActivity[visibleActivity.length - 1]?.date}
            </span>
            <div className="flex items-center gap-3">
              <span className="flex items-center gap-1">
                <span className="h-2.5 w-2.5 rounded-xs border border-zinc-800 bg-zinc-900" /> Idle
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
  )
}

export function StorageAnalyticsCard({ storageAnalytics = {} }) {
  const providerBreakdown = storageAnalytics.byProvider || []
  const totalProviderBytes = Math.max(
    1,
    providerBreakdown.reduce((sum, p) => sum + (p.totalBytes || 0), 0),
  )
  const topTargets = storageAnalytics.byTarget || []
  const maxTargetBytes = Math.max(1, ...topTargets.slice(0, 5).map((t) => t.totalBytes || 0))
  const disk = storageAnalytics.disk
  const diskUsedPct =
    disk && disk.totalBytes > 0 ? Math.round((disk.usedBytes / disk.totalBytes) * 100) : 0

  return (
    <Card className="h-full flex flex-col justify-between p-5 gap-4">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="flex items-center gap-2">
          <BarChart3 className="h-4 w-4 text-sky-400 shrink-0" />
          <div>
            <h3 className="text-sm font-semibold text-zinc-100">Storage Analytics &amp; Footprint</h3>
            <p className="text-xs text-zinc-400">
              Host disk headroom, destinations, and top space consumers
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

      <div className="flex-1 flex flex-col gap-3">
        {disk && disk.totalBytes > 0 && (
          <div className="rounded-xl border border-zinc-800/80 bg-zinc-950/60 p-3 space-y-2">
            <div className="flex flex-wrap items-center justify-between gap-2 text-xs">
              <span className="text-zinc-300 font-medium">
                Host Disk (<span className="font-mono text-zinc-400">{disk.path}</span>)
              </span>
              <span className="font-mono text-zinc-200 tabular-nums">
                {formatBytes(disk.freeBytes)} free · {diskUsedPct}% of {formatBytes(disk.totalBytes)}
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

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 flex-1">
          <div className="rounded-xl border border-zinc-800/80 bg-zinc-950/60 p-3 space-y-2.5">
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
                      <span className="shrink-0 font-mono text-[11px] text-zinc-400 tabular-nums">
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

          <div className="rounded-xl border border-zinc-800/80 bg-zinc-950/60 p-3 space-y-2.5">
            <div className="text-[11px] font-medium uppercase tracking-wider text-zinc-400">
              Top Space Consumers
            </div>
            {topTargets.length === 0 ? (
              <p className="text-xs text-zinc-500">Run backups to track size per target.</p>
            ) : (
              <div className="space-y-2 text-xs">
                {topTargets.slice(0, 5).map((t) => {
                  const pct = Math.round(((t.totalBytes || 0) / maxTargetBytes) * 100)
                  return (
                    <div key={`${t.targetType}-${t.targetName}`} className="space-y-1">
                      <div className="flex items-center justify-between gap-2">
                        <span className="flex items-center gap-1.5 min-w-0">
                          <Badge variant="neutral" className="text-[10px] py-0 px-1.5 shrink-0">
                            {t.targetType}
                          </Badge>
                          <span className="truncate text-zinc-200">{t.targetName}</span>
                        </span>
                        <span className="shrink-0 font-mono text-[11px] text-zinc-400 tabular-nums">
                          {formatBytes(t.totalBytes)} ({t.backupCount}×)
                        </span>
                      </div>
                      <div className="h-1.5 w-full overflow-hidden rounded-full bg-zinc-900">
                        <div
                          style={{ width: `${Math.max(pct, t.totalBytes > 0 ? 4 : 0)}%` }}
                          className="h-full rounded-full bg-violet-500/80"
                        />
                      </div>
                    </div>
                  )
                })}
              </div>
            )}
          </div>
        </div>
      </div>

      <div className="flex items-center justify-between text-[11px] text-zinc-400">
        <span>{providerBreakdown.length} storage destination(s)</span>
        <span>
          {topTargets.length} tracked backup target{topTargets.length === 1 ? '' : 's'}
        </span>
      </div>
    </Card>
  )
}
