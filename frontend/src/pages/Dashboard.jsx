import { Activity, CalendarClock, Container, Database, Layers } from 'lucide-react'
import { formatBytes, summarizeNotificationsList } from '../api/client'
import { SetupChecklist } from '../components/SetupChecklist'
import {
  BackupActivityCard,
  StorageAnalyticsCard,
} from '../components/dashboard/AnalyticsRow'
import {
  ActiveRollbacksBanner,
  InfrastructureStrip,
  RecentBackupsCard,
  RecentRestoresCard,
  UpcomingSchedulesCard,
} from '../components/dashboard/DashboardCards'
import { Badge, Button, Card } from '../components/ui'

export function Dashboard({ dashboard, providers = [], notifications = [], onNavigate }) {
  const notifOverview = summarizeNotificationsList(notifications)
  const stats = dashboard?.stats || {}
  const docker = dashboard?.docker || {}
  const recentBackups = dashboard?.recentBackups || []
  const recentRestores = dashboard?.recentRestores || []
  const activeRollbacks = dashboard?.activeRollbacks || []
  const upcomingSchedules = dashboard?.upcomingSchedules || []
  const scheduleCoverage = dashboard?.scheduleCoverage || {}
  const dailyActivity = dashboard?.dailyActivity || []
  const storageAnalytics = dashboard?.storageAnalytics || {}
  const securityOverview = dashboard?.securityOverview || {}

  const encPct =
    securityOverview.totalSuccessfulBackups > 0
      ? Math.round(
          ((securityOverview.encryptedBackups || 0) / securityOverview.totalSuccessfulBackups) *
            100,
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
      hint:
        (scheduleCoverage.stacksTotal ?? 0) + (scheduleCoverage.containersTotal ?? 0) > 0
          ? `${scheduleCoverage.stacksScheduled ?? 0}/${scheduleCoverage.stacksTotal ?? 0} stacks · ${scheduleCoverage.containersScheduled ?? 0}/${scheduleCoverage.containersTotal ?? 0} containers covered`
          : 'Automated cron jobs',
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
                  : docker.error ||
                    'Mount /var/run/docker.sock to discover containers and volumes.'}
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

      {/* KPI strip */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        {statCards.map((item) => {
          const Icon = item.icon
          return (
            <Card key={item.label} className="p-4">
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium text-zinc-400">{item.label}</span>
                <Icon className={`h-4 w-4 ${item.color}`} />
              </div>
              <div className="mt-2 text-2xl font-semibold tracking-tight text-zinc-100 tabular-nums">
                {item.value}
              </div>
              <p className="mt-1 text-xs text-zinc-500">{item.hint}</p>
            </Card>
          )
        })}
      </div>

      <ActiveRollbacksBanner
        activeRollbacks={activeRollbacks}
        onViewHistory={() => onNavigate('history')}
      />

      {/* Row 1: Aligned Analytics & Storage Charts */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6 items-stretch">
        <BackupActivityCard dailyActivity={dailyActivity} />
        <StorageAnalyticsCard storageAnalytics={storageAnalytics} />
      </div>

      {/* Row 2: Recent Backups, Rollbacks & Upcoming Schedules */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6 items-stretch">
        <RecentBackupsCard recentBackups={recentBackups} onNavigate={onNavigate} />
        <RecentRestoresCard
          recentRestores={recentRestores}
          activeRollbacks={activeRollbacks}
          onNavigate={onNavigate}
        />
        <UpcomingSchedulesCard upcomingSchedules={upcomingSchedules} onNavigate={onNavigate} />
      </div>

      {/* Row 3: Infrastructure, Encryption Health & Alert Notifications */}
      <InfrastructureStrip
        providers={providers}
        securityOverview={securityOverview}
        encPct={encPct}
        notifOverview={notifOverview}
        onNavigate={onNavigate}
      />
    </div>
  )
}
