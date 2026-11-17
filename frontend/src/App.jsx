import { useCallback, useEffect, useRef, useState } from 'react'
import {
  Activity,
  Bell,
  BellOff,
  CalendarClock,
  Container,
  History as HistoryIcon,
  LayoutDashboard,
  LogOut,
  RefreshCw,
  Settings as SettingsIcon,
  Shield,
  User,
} from 'lucide-react'
import {
  api,
  formatBytes,
  setTimeFormatPreference,
  setUnauthorizedHandler,
  summarizeNotificationsList,
} from './api/client'
import { BackupModal } from './components/BackupModal'
import { LiveProgressDrawer } from './components/LiveProgressDrawer'
import { ScheduleModal } from './components/ScheduleModal'
import { Badge, Button, ToastContainer } from './components/ui'
import { Containers } from './pages/Containers'
import { Dashboard } from './pages/Dashboard'
import { History } from './pages/History'
import { Login } from './pages/Login'
import { Schedules } from './pages/Schedules'
import { Settings } from './pages/Settings'

const NAV_ITEMS = [
  { id: 'dashboard', label: 'Dashboard', icon: LayoutDashboard },
  { id: 'containers', label: 'Containers & Volumes', icon: Container },
  { id: 'schedules', label: 'Schedules', icon: CalendarClock },
  { id: 'history', label: 'History', icon: HistoryIcon },
  { id: 'settings', label: 'Settings', icon: SettingsIcon },
]

const DEFAULT_PREFS = {
  theme: 'dark',
  accentColor: 'sky',
  density: 'comfortable',
  instanceName: '',
  defaultPage: 'dashboard',
  defaultResourceTab: 'stacks',
  showStoppedDefault: true,
  autoRefreshSeconds: 0,
  timeFormat: '24h',
  defaultPauseBackups: true,
}

function applyDocumentTheme(prefs) {
  const root = document.documentElement
  let resolvedTheme = prefs.theme || 'dark'
  if (resolvedTheme === 'system' && typeof window !== 'undefined' && window.matchMedia) {
    resolvedTheme = window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark'
  }
  root.setAttribute('data-theme', resolvedTheme)
  root.setAttribute('data-accent', prefs.accentColor || 'sky')
  root.setAttribute('data-density', prefs.density || 'comfortable')
  setTimeFormatPreference(prefs.timeFormat || '24h')
}

export default function App() {
  const [page, setPage] = useState('dashboard')
  const [settingsSection, setSettingsSection] = useState('storage')
  const [includeStopped, setIncludeStopped] = useState(true)
  const [loading, setLoading] = useState(true)
  const [authChecking, setAuthChecking] = useState(true)
  const [preferences, setPreferences] = useState(DEFAULT_PREFS)
  const initializedPrefsRef = useRef(false)

  const [authStatus, setAuthStatus] = useState({
    enabled: false,
    authenticated: true,
    username: '',
    authMode: 'none',
    hasUsers: false,
    trustedProxyHeader: '',
  })

  const [dashboard, setDashboard] = useState(null)
  const [containers, setContainers] = useState([])
  const [stacks, setStacks] = useState([])
  const [volumes, setVolumes] = useState([])
  const [schedules, setSchedules] = useState([])
  const [backups, setBackups] = useState([])
  const [providers, setProviders] = useState([])
  const [notifications, setNotifications] = useState([])

  const [backupTarget, setBackupTarget] = useState(null)
  const [scheduleModal, setScheduleModal] = useState({ open: false, initialTarget: null, existing: null })
  const [liveJobs, setLiveJobs] = useState([])
  const [progressDrawerOpen, setProgressDrawerOpen] = useState(false)
  const prevRunningIdsRef = useRef(new Set())
  const [toasts, setToasts] = useState([])

  const notify = useCallback((type, message) => {
    const id = `${Date.now()}-${Math.random().toString(36).slice(2, 7)}`
    setToasts((prev) => [...prev, { id, type, message }])
    setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id))
    }, 4500)
  }, [])

  const applyPreferences = useCallback((nextPrefs, applyInitialNavigation = false) => {
    const merged = { ...DEFAULT_PREFS, ...(nextPrefs || {}) }
    setPreferences(merged)
    applyDocumentTheme(merged)
    if (applyInitialNavigation && !initializedPrefsRef.current) {
      initializedPrefsRef.current = true
      if (merged.defaultPage) setPage(merged.defaultPage)
      setIncludeStopped(merged.showStoppedDefault ?? true)
    }
  }, [])

  useEffect(() => {
    applyDocumentTheme(preferences)
    if (preferences.theme !== 'system' || !window.matchMedia) return
    const mq = window.matchMedia('(prefers-color-scheme: light)')
    const onChange = () => applyDocumentTheme(preferences)
    mq.addEventListener?.('change', onChange)
    return () => mq.removeEventListener?.('change', onChange)
  }, [preferences])

  const checkAuth = useCallback(async () => {
    try {
      const res = await api.getAuthStatus()
      setAuthStatus(res)
      if (res?.preferences) {
        applyPreferences(res.preferences, true)
      }
      return res
    } catch {
      return { enabled: false, authenticated: true }
    } finally {
      setAuthChecking(false)
    }
  }, [applyPreferences])

  useEffect(() => {
    setUnauthorizedHandler(() => {
      setAuthStatus((prev) => ({ ...prev, enabled: true, authenticated: false }))
    })
    checkAuth()
  }, [checkAuth])

  const loadAll = useCallback(
    async (silent = false) => {
      if (authChecking || (authStatus.enabled && !authStatus.authenticated)) {
        return
      }
      if (!silent) setLoading(true)
      try {
        const [dashRes, schedRes, backupRes, storageRes, prefRes, notifRes] = await Promise.all([
          api.getDashboard(),
          api.getSchedules(),
          api.getBackups({ limit: 150 }),
          api.getStorageProviders(),
          api.getUIPreferences().catch(() => null),
          api.getNotifications().catch(() => null),
        ])
        setDashboard(dashRes)
        setSchedules(schedRes.schedules || [])
        setBackups(backupRes.backups || [])
        setProviders(storageRes.providers || [])
        if (prefRes?.preferences) {
          applyPreferences(prefRes.preferences, !initializedPrefsRef.current)
        }
        if (notifRes) {
          const list = Array.isArray(notifRes.notifications)
            ? notifRes.notifications
            : notifRes.notification
              ? [notifRes.notification]
              : []
          setNotifications(list)
        }

        if (dashRes?.docker?.connected) {
          const [ctrRes, stackRes, volRes] = await Promise.all([
            api.getContainers(includeStopped),
            api.getStacks(includeStopped),
            api.getVolumes(),
          ])
          setContainers(ctrRes.containers || [])
          setStacks(stackRes.stacks || [])
          setVolumes(volRes.volumes || [])
        } else {
          setContainers([])
          setStacks([])
          setVolumes([])
        }
      } catch (err) {
        if (!silent) notify('error', err.message)
      } finally {
        if (!silent) setLoading(false)
      }
    },
    [authChecking, authStatus.enabled, authStatus.authenticated, includeStopped, notify, applyPreferences],
  )

  useEffect(() => {
    loadAll()
  }, [loadAll])

  // Subscribe to SSE live backup progress events when authenticated.
  useEffect(() => {
    if (authChecking || (authStatus.enabled && !authStatus.authenticated)) return undefined
    const es = new EventSource(api.backupEventsUrl())
    es.addEventListener('progress', (e) => {
      try {
        const data = JSON.parse(e.data)
        const jobs = Array.isArray(data.jobs) ? data.jobs : []
        setLiveJobs(jobs)

        const currentRunning = new Set(jobs.filter((j) => j.status === 'running').map((j) => j.backupId))
        let finishedAny = false
        for (const id of prevRunningIdsRef.current) {
          if (!currentRunning.has(id)) {
            finishedAny = true
            break
          }
        }
        prevRunningIdsRef.current = currentRunning
        if (finishedAny) {
          loadAll(true)
        }
      } catch {
        // Ignore malformed progress frames.
      }
    })
    return () => {
      es.close()
    }
  }, [authChecking, authStatus.enabled, authStatus.authenticated, loadAll])

  // Fast poll while any backup job is running; optional background auto-refresh from Settings.
  const activeLiveJobs = liveJobs.filter((j) => j.status === 'running')
  const hasRunningBackup = activeLiveJobs.length > 0 || backups.some((b) => b.status === 'running')
  useEffect(() => {
    if (authStatus.enabled && !authStatus.authenticated) return
    const intervalMs = hasRunningBackup
      ? 2500
      : preferences.autoRefreshSeconds > 0
        ? preferences.autoRefreshSeconds * 1000
        : 0
    if (!intervalMs) return
    const timer = setInterval(() => {
      loadAll(true)
    }, intervalMs)
    return () => clearInterval(timer)
  }, [hasRunningBackup, preferences.autoRefreshSeconds, authStatus.enabled, authStatus.authenticated, loadAll])

  // Press 'r' outside input fields to refresh quickly.
  useEffect(() => {
    const onKey = (e) => {
      if (
        e.key === 'r' &&
        !e.metaKey &&
        !e.ctrlKey &&
        !e.altKey &&
        !['INPUT', 'TEXTAREA', 'SELECT'].includes(document.activeElement?.tagName)
      ) {
        loadAll(true)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [loadAll])

  async function handleLogout() {
    await api.logout()
    setAuthStatus((prev) => ({ ...prev, authenticated: false, username: '' }))
  }

  if (authChecking) {
    return (
      <div className="min-h-screen flex items-center justify-center text-sm text-zinc-400">
        Loading DockVault...
      </div>
    )
  }

  if (authStatus.enabled && !authStatus.authenticated) {
    return (
      <>
        <Login
          instanceName={preferences.instanceName}
          onLoggedIn={(res) => {
            setAuthStatus((prev) => ({
              ...prev,
              authenticated: true,
              username: res.username || prev.username,
              authMode: 'local',
            }))
          }}
        />
        <ToastContainer
          toasts={toasts}
          onDismiss={(id) => setToasts((prev) => prev.filter((t) => t.id !== id))}
        />
      </>
    )
  }

  const dockerConnected = dashboard?.docker?.connected
  const notifOverview = summarizeNotificationsList(notifications)

  function navigateTo(nextPage, section) {
    if (nextPage === 'settings' && section) {
      setSettingsSection(section)
    }
    setPage(nextPage)
  }

  return (
    <div className="min-h-screen flex flex-col">
      {/* Top Navbar */}
      <header
        style={{
          backgroundColor: 'var(--dv-header)',
          borderColor: 'var(--dv-border)',
        }}
        className="sticky top-0 z-30 border-b backdrop-blur-md"
      >
        <div className="mx-auto flex max-w-7xl items-center justify-between px-4 sm:px-6 h-16">
          <div className="flex items-center gap-3">
            <div
              style={{
                backgroundColor: 'var(--dv-accent-soft)',
                borderColor: 'var(--dv-accent-border)',
                color: 'var(--dv-accent-text)',
              }}
              className="flex h-9 w-9 items-center justify-center rounded-xl border"
            >
              <Shield className="h-5 w-5" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <span className="text-base font-bold tracking-tight text-zinc-100">DockVault</span>
                {preferences.instanceName && (
                  <Badge variant="info" className="font-mono text-[10px]">
                    {preferences.instanceName}
                  </Badge>
                )}
                <span
                  style={{ borderColor: 'var(--dv-border)' }}
                  className="rounded-md border bg-zinc-900 px-1.5 py-0.5 font-mono text-[10px] text-zinc-400"
                >
                  {dashboard?.version || 'dev'}
                </span>
              </div>
              <p className="text-[11px] text-zinc-400 hidden sm:block">
                Docker Volume &amp; Compose Backup UI
              </p>
            </div>
          </div>

          <div className="flex items-center gap-2.5">
            {(hasRunningBackup || liveJobs.length > 0) && (
              <button
                type="button"
                onClick={() => setProgressDrawerOpen(true)}
                title="View live backup step progress & logs"
                className="cursor-pointer transition-opacity hover:opacity-85"
              >
                <Badge variant={hasRunningBackup ? 'info' : 'neutral'} className={hasRunningBackup ? 'animate-pulse' : ''}>
                  <Activity className="h-3 w-3" />
                  {activeLiveJobs[0]
                    ? `${activeLiveJobs[0].targetName}: ${activeLiveJobs[0].phaseLabel || 'Running'}${
                        activeLiveJobs[0].bytesProcessed > 0
                          ? ` · ${formatBytes(activeLiveJobs[0].bytesProcessed)}`
                          : ''
                      }`
                    : hasRunningBackup
                      ? 'Backup in progress…'
                      : `Recent activity (${liveJobs.length})`}
                </Badge>
              </button>
            )}
            <Badge variant={dockerConnected ? 'success' : 'danger'}>
              <span
                className={`h-1.5 w-1.5 rounded-full ${
                  dockerConnected ? 'bg-emerald-400' : 'bg-rose-400'
                }`}
              />
              {dockerConnected ? 'Docker Online' : 'Docker Offline'}
            </Badge>
            <button
              type="button"
              onClick={() => navigateTo('settings', 'notifications')}
              title={
                notifOverview.activeCount > 0
                  ? `Active alert channels (${notifOverview.activeCount}): ${notifOverview.activeChannels
                      .map((c) => `${c.displayName} (${c.summary.targetSummary})`)
                      .join(', ')}`
                  : notifOverview.configuredCount > 0
                    ? `${notifOverview.configuredCount} alert channel(s) configured (all paused)`
                    : 'No alert channels configured — click to add one'
              }
              className="cursor-pointer transition-opacity hover:opacity-85"
            >
              <Badge
                variant={
                  notifOverview.activeCount > 0
                    ? 'success'
                    : notifOverview.configuredCount > 0
                      ? 'warning'
                      : 'neutral'
                }
              >
                {notifOverview.activeCount > 0 ? (
                  <Bell className="h-3 w-3" />
                ) : (
                  <BellOff className="h-3 w-3" />
                )}
                <span className="hidden md:inline">
                  {notifOverview.activeCount === 1
                    ? `Alerts: ${notifOverview.firstActive.displayName}`
                    : notifOverview.activeCount > 1
                      ? `Alerts: ${notifOverview.activeCount} Active`
                      : notifOverview.configuredCount > 0
                        ? `Alerts Paused (${notifOverview.configuredCount})`
                        : 'Alerts Off'}
                </span>
              </Badge>
            </button>
            {authStatus.enabled && authStatus.username && (
              <Badge variant="neutral">
                <User className="h-3 w-3" />
                {authStatus.username}
              </Badge>
            )}
            <Button
              variant="ghost"
              size="sm"
              title="Refresh (R)"
              onClick={() => loadAll()}
              disabled={loading}
            >
              <RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
            </Button>
            {authStatus.enabled && authStatus.authMode === 'local' && (
              <Button variant="ghost" size="sm" title="Sign out" onClick={handleLogout}>
                <LogOut className="h-3.5 w-3.5" />
              </Button>
            )}
          </div>
        </div>

        {/* Sub-navigation tabs */}
        <div className="mx-auto max-w-7xl px-4 sm:px-6">
          <nav className="flex items-center gap-1 overflow-x-auto -mb-px">
            {NAV_ITEMS.map((item) => {
              const Icon = item.icon
              const active = page === item.id
              return (
                <button
                  key={item.id}
                  type="button"
                  onClick={() => setPage(item.id)}
                  style={
                    active
                      ? {
                          borderColor: 'var(--dv-accent)',
                          color: 'var(--dv-accent-text)',
                        }
                      : undefined
                  }
                  className={`flex items-center gap-2 whitespace-nowrap border-b-2 px-3.5 py-2.5 text-xs font-medium transition-colors cursor-pointer ${
                    active
                      ? ''
                      : 'border-transparent text-zinc-400 hover:border-zinc-700 hover:text-zinc-200'
                  }`}
                >
                  <Icon className="h-3.5 w-3.5" />
                  {item.label}
                </button>
              )
            })}
          </nav>
        </div>
      </header>

      {/* Main Content */}
      <main className="mx-auto w-full max-w-7xl flex-1 px-4 sm:px-6 py-6">
        {page === 'dashboard' && (
          <Dashboard
            dashboard={dashboard}
            providers={providers}
            notifications={notifications}
            onNavigate={navigateTo}
          />
        )}

        {page === 'containers' && (
          <Containers
            containers={containers}
            stacks={stacks}
            volumes={volumes}
            loading={loading}
            defaultTab={preferences.defaultResourceTab}
            includeStopped={includeStopped}
            onToggleIncludeStopped={setIncludeStopped}
            onRefresh={() => loadAll()}
            onOpenBackup={(target) => setBackupTarget(target)}
            onOpenSchedule={(target) =>
              setScheduleModal({ open: true, initialTarget: target, existing: null })
            }
            notify={notify}
          />
        )}

        {page === 'schedules' && (
          <Schedules
            schedules={schedules}
            onCreate={() => setScheduleModal({ open: true, initialTarget: null, existing: null })}
            onEdit={(sched) => setScheduleModal({ open: true, initialTarget: null, existing: sched })}
            onRefresh={() => loadAll(true)}
            notify={notify}
          />
        )}

        {page === 'history' && (
          <History
            backups={backups}
            loading={loading}
            onRefresh={() => loadAll()}
            notify={notify}
          />
        )}

        {page === 'settings' && (
          <Settings
            providers={providers}
            dashboard={dashboard}
            authStatus={authStatus}
            preferences={preferences}
            notifications={notifications}
            activeSection={settingsSection}
            onSectionChange={setSettingsSection}
            onPreferencesChange={(nextPrefs) => applyPreferences(nextPrefs, false)}
            onNotificationsChange={setNotifications}
            onAuthChange={(nextAuth) => setAuthStatus(nextAuth)}
            onRefresh={() => loadAll(true)}
            notify={notify}
          />
        )}
      </main>

      {/* Modals & Toasts */}
      <BackupModal
        open={Boolean(backupTarget)}
        onClose={() => setBackupTarget(null)}
        target={backupTarget}
        providers={providers}
        defaultPauseBackups={preferences.defaultPauseBackups}
        onStarted={() => {
          setProgressDrawerOpen(true)
          loadAll(true)
        }}
        notify={notify}
      />

      <LiveProgressDrawer
        open={progressDrawerOpen}
        onClose={() => setProgressDrawerOpen(false)}
        jobs={liveJobs}
      />

      <ScheduleModal
        open={scheduleModal.open}
        onClose={() => setScheduleModal({ open: false, initialTarget: null, existing: null })}
        initialTarget={scheduleModal.initialTarget}
        existingSchedule={scheduleModal.existing}
        providers={providers}
        containers={containers}
        stacks={stacks}
        volumes={volumes}
        defaultPauseBackups={preferences.defaultPauseBackups}
        onSaved={() => loadAll(true)}
        notify={notify}
      />

      <ToastContainer
        toasts={toasts}
        onDismiss={(id) => setToasts((prev) => prev.filter((t) => t.id !== id))}
      />
    </div>
  )
}
