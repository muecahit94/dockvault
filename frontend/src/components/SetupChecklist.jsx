import { useState } from 'react'
import { Bell, CalendarClock, CheckCircle2, Circle, Cloud, Play, ShieldCheck, Sparkles, X } from 'lucide-react'
import { Button, Card } from './ui'

const DISMISS_KEY = 'dockvault.setupChecklistDismissed'

function readDismissed() {
  try {
    return window.localStorage.getItem(DISMISS_KEY) === 'true'
  } catch {
    return false
  }
}

// First-run guide: hidden once every step is complete or the user dismisses it.
export function SetupChecklist({ dashboard, providers, activeChannelCount, onNavigate }) {
  const [dismissed, setDismissed] = useState(readDismissed)
  if (!dashboard || dismissed) return null

  const stats = dashboard.stats || {}
  const security = dashboard.securityOverview || {}

  const steps = [
    {
      id: 'auth',
      icon: ShieldCheck,
      title: 'Secure access',
      description: 'DockVault controls Docker — enable login or a trusted SSO proxy header.',
      done: Boolean(security.authEnabled || security.trustedProxyHeader),
      action: 'Configure',
      go: () => onNavigate('settings', 'security'),
    },
    {
      id: 'backup',
      icon: Play,
      title: 'Run your first backup',
      description: 'Pick a stack, container, or volume and back it up once to confirm everything works.',
      done: (stats.successfulBackups ?? 0) > 0,
      action: 'Browse targets',
      go: () => onNavigate('containers'),
    },
    {
      id: 'schedule',
      icon: CalendarClock,
      title: 'Automate with a schedule',
      description: 'Create a cron schedule with retention — and set an overdue alert window.',
      done: (stats.activeSchedules ?? 0) > 0,
      action: 'Create schedule',
      go: () => onNavigate('schedules'),
    },
    {
      id: 'offsite',
      icon: Cloud,
      title: 'Add offsite storage',
      description: 'Follow 3-2-1: replicate archives to S3, R2, B2, or Azure Blob Storage.',
      done: providers.some((p) => p.type !== 'local'),
      action: 'Add storage',
      go: () => onNavigate('settings', 'storage'),
    },
    {
      id: 'alerts',
      icon: Bell,
      title: 'Turn on failure alerts',
      description: 'Get notified on Telegram, Discord, Slack, ntfy, or a webhook when a backup fails or stops.',
      done: activeChannelCount > 0,
      action: 'Add channel',
      go: () => onNavigate('settings', 'notifications'),
    },
  ]

  const completed = steps.filter((s) => s.done).length
  if (completed === steps.length) return null

  function dismiss() {
    try {
      window.localStorage.setItem(DISMISS_KEY, 'true')
    } catch {
      // Ignore storage failures; dismissal then lasts for this session only.
    }
    setDismissed(true)
  }

  return (
    <Card className="p-5 space-y-4">
      <div className="flex items-start justify-between gap-3">
        <div className="flex items-start gap-2.5">
          <Sparkles className="mt-0.5 h-4 w-4 text-sky-400" />
          <div>
            <h3 className="text-sm font-semibold text-zinc-100">Get set up</h3>
            <p className="text-xs text-zinc-400">
              {completed} of {steps.length} steps complete — finish these to make your backups reliable.
            </p>
          </div>
        </div>
        <Button variant="ghost" size="sm" title="Dismiss checklist" onClick={dismiss}>
          <X className="h-3.5 w-3.5" />
        </Button>
      </div>

      <div className="h-1.5 w-full overflow-hidden rounded-full bg-zinc-800">
        <div
          className="h-full rounded-full bg-emerald-500 transition-all"
          style={{ width: `${Math.round((completed / steps.length) * 100)}%` }}
        />
      </div>

      <div className="divide-y divide-zinc-800/70">
        {steps.map((step) => {
          const Icon = step.icon
          return (
            <div key={step.id} className="flex items-center justify-between gap-3 py-2.5">
              <div className="flex items-start gap-2.5">
                {step.done ? (
                  <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-emerald-400" />
                ) : (
                  <Circle className="mt-0.5 h-4 w-4 shrink-0 text-zinc-600" />
                )}
                <div>
                  <div
                    className={`flex items-center gap-1.5 text-xs font-medium ${
                      step.done ? 'text-zinc-500 line-through' : 'text-zinc-100'
                    }`}
                  >
                    <Icon className="h-3.5 w-3.5" />
                    {step.title}
                  </div>
                  {!step.done && <p className="mt-0.5 text-[11px] text-zinc-400">{step.description}</p>}
                </div>
              </div>
              {!step.done && (
                <Button variant="outline" size="sm" onClick={step.go}>
                  {step.action}
                </Button>
              )}
            </div>
          )
        })}
      </div>
    </Card>
  )
}
