import { CalendarClock, Edit2, Play, Plus, Trash2 } from 'lucide-react'
import { api, formatDateTime, formatRelativeTime } from '../api/client'
import { Badge, Button, Card, EmptyState, Switch } from '../components/ui'

export function Schedules({ schedules, onCreate, onEdit, onRefresh, notify }) {
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
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold text-zinc-100">Backup Schedules</h2>
          <p className="text-xs text-zinc-400">
            Automated cron jobs with configurable retention policies
          </p>
        </div>
        <Button onClick={onCreate}>
          <Plus className="h-4 w-4" />
          New Schedule
        </Button>
      </div>

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
                      <div className="font-medium text-zinc-100">{s.name}</div>
                      <div className="mt-0.5 text-xs text-zinc-500">
                        {s.lastRunAt ? (
                          <span>
                            Last: {formatDateTime(s.lastRunAt)} ({s.lastStatus || 'done'})
                          </span>
                        ) : (
                          'Never run yet'
                        )}
                      </div>
                    </td>
                    <td className="px-4 py-3.5">
                      <div className="flex items-center gap-1.5">
                        <Badge variant="info">{s.targetType}</Badge>
                        <span className="text-zinc-200">{s.targetName}</span>
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
    </div>
  )
}
