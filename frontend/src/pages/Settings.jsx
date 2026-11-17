import { useEffect, useState } from 'react'
import {
  Bell,
  BellOff,
  Cloud,
  Container,
  DownloadCloud,
  Edit2,
  FolderSearch,
  HardDrive,
  Lock,
  Monitor,
  Moon,
  Palette,
  PlugZap,
  Plus,
  RotateCcw,
  Send,
  Server,
  ShieldCheck,
  Sun,
  Trash2,
} from 'lucide-react'
import { api, formatBytes, formatDateTime, summarizeNotificationsList } from '../api/client'
import { Badge, Button, Card, EmptyState, Input, Label, Modal, Select, Switch } from '../components/ui'

const EMPTY_FORM = {
  name: '',
  type: 's3',
  isDefault: false,
  localPath: '',
  endpoint: '',
  region: 'us-east-1',
  bucket: '',
  prefix: 'dockvault',
  accessKey: '',
  secretKey: '',
  useSSL: true,
  azureAuthType: 'key',
  azureAccountName: '',
  azureTenantId: '',
  azureClientId: '',
  azureContainer: '',
  azureAccessTier: 'Cool',
  azureAutoCreateContainer: true,
  encryptBackups: false,
  encryptionPassphrase: '',
}

const EMPTY_NOTIF_FORM = {
  name: '',
  enabled: true,
  provider: 'telegram',
  webhookUrl: '',
  telegramBotToken: '',
  hasTelegramBotToken: false,
  telegramChatId: '',
  telegramThreadId: '',
  onSuccess: false,
  onFailure: true,
}

const AZURE_TIERS = [
  { value: 'Cool', label: 'Cool — Recommended for backups (30+ day min)' },
  { value: 'Cold', label: 'Cold — Lower storage cost (90+ day min)' },
  { value: 'Hot', label: 'Hot — Frequent access / fast restores' },
  { value: 'Archive', label: 'Archive — Lowest cost (requires rehydration)' },
]

const ACCENT_OPTIONS = [
  { id: 'sky', label: 'Sky', swatch: '#0ea5e9' },
  { id: 'emerald', label: 'Emerald', swatch: '#10b981' },
  { id: 'violet', label: 'Violet', swatch: '#8b5cf6' },
  { id: 'amber', label: 'Amber', swatch: '#f59e0b' },
  { id: 'rose', label: 'Rose', swatch: '#f43f5e' },
]

const SETTINGS_TABS = [
  { id: 'storage', label: 'Storage', icon: HardDrive },
  { id: 'appearance', label: 'Appearance & UI', icon: Palette },
  { id: 'security', label: 'Security & Auth', icon: ShieldCheck },
  { id: 'notifications', label: 'Notifications', icon: Bell },
  { id: 'system', label: 'System', icon: Server },
]

export function Settings({
  providers,
  dashboard,
  authStatus,
  preferences,
  notifications = [],
  activeSection,
  onSectionChange,
  onPreferencesChange,
  onNotificationsChange,
  onAuthChange,
  onRefresh,
  notify,
}) {
  const [localSection, setLocalSection] = useState(activeSection || 'storage')
  const section = activeSection ?? localSection
  const setSection = (next) => {
    setLocalSection(next)
    onSectionChange?.(next)
  }

  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState(null)
  const [form, setForm] = useState(EMPTY_FORM)
  const [testingId, setTestingId] = useState(null)
  const [testingForm, setTestingForm] = useState(false)
  const [saving, setSaving] = useState(false)

  // Scan & Import modal state
  const [scanProvider, setScanProvider] = useState(null)
  const [scanLoading, setScanLoading] = useState(false)
  const [scannedObjects, setScannedObjects] = useState([])
  const [selectedImportPaths, setSelectedImportPaths] = useState([])
  const [importing, setImporting] = useState(false)

  // Appearance & UI preferences draft
  const [prefsDraft, setPrefsDraft] = useState(preferences || {})
  const [savingPrefs, setSavingPrefs] = useState(false)

  useEffect(() => {
    if (preferences) setPrefsDraft(preferences)
  }, [preferences])

  // Auth settings state
  const [authEnabled, setAuthEnabled] = useState(Boolean(authStatus?.enabled))
  const [authUser, setAuthUser] = useState(authStatus?.username || 'admin')
  const [authPass, setAuthPass] = useState('')
  const [proxyHeader, setProxyHeader] = useState(authStatus?.trustedProxyHeader || '')
  const [savingAuth, setSavingAuth] = useState(false)

  // Multi-channel notifications state
  const [notifList, setNotifList] = useState(notifications)
  const [notifModalOpen, setNotifModalOpen] = useState(false)
  const [editingNotif, setEditingNotif] = useState(null)
  const [notifForm, setNotifForm] = useState(EMPTY_NOTIF_FORM)
  const [savingNotif, setSavingNotif] = useState(false)
  const [testingNotifId, setTestingNotifId] = useState(null)
  const [testingNotifModal, setTestingNotifModal] = useState(false)

  useEffect(() => {
    setAuthEnabled(Boolean(authStatus?.enabled))
    if (authStatus?.username) setAuthUser(authStatus.username)
    setProxyHeader(authStatus?.trustedProxyHeader || '')
  }, [authStatus])

  useEffect(() => {
    setNotifList(notifications || [])
  }, [notifications])

  useEffect(() => {
    api
      .getNotifications()
      .then((res) => {
        const list = Array.isArray(res?.notifications)
          ? res.notifications
          : res?.notification
            ? [res.notification]
            : []
        setNotifList(list)
        onNotificationsChange?.(list)
      })
      .catch(() => {})
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function updatePrefPreview(patch) {
    const next = { ...prefsDraft, ...patch }
    setPrefsDraft(next)
    onPreferencesChange?.(next)
  }

  async function handleSavePreferences(e) {
    e.preventDefault()
    setSavingPrefs(true)
    try {
      const res = await api.updateUIPreferences(prefsDraft)
      onPreferencesChange?.(res.preferences)
      notify('success', 'UI & workspace preferences saved')
    } catch (err) {
      notify('error', err.message)
    } finally {
      setSavingPrefs(false)
    }
  }

  async function handleResetPreferences() {
    setSavingPrefs(true)
    try {
      const res = await api.resetUIPreferences()
      setPrefsDraft(res.preferences)
      onPreferencesChange?.(res.preferences)
      notify('success', 'Preferences reset to defaults')
    } catch (err) {
      notify('error', err.message)
    } finally {
      setSavingPrefs(false)
    }
  }

  function openCreate() {
    setEditing(null)
    setForm(EMPTY_FORM)
    setModalOpen(true)
  }

  function openEdit(provider) {
    setEditing(provider)
    setForm({
      name: provider.name || '',
      type: provider.type || 'local',
      isDefault: Boolean(provider.isDefault),
      localPath: provider.localPath || '',
      endpoint: provider.endpoint || '',
      region: provider.region || '',
      bucket: provider.bucket || '',
      prefix: provider.prefix || '',
      accessKey: provider.accessKey || '',
      secretKey: '',
      useSSL: provider.useSSL ?? true,
      azureAuthType: provider.azureAuthType || 'key',
      azureAccountName: provider.azureAccountName || '',
      azureTenantId: provider.azureTenantId || '',
      azureClientId: provider.azureClientId || '',
      azureContainer: provider.azureContainer || '',
      azureAccessTier: provider.azureAccessTier || 'Cool',
      azureAutoCreateContainer: provider.azureAutoCreateContainer ?? true,
      encryptBackups: Boolean(provider.encryptBackups),
      encryptionPassphrase: '',
    })
    setModalOpen(true)
  }

  function formatTestToast(res) {
    if (!res?.message) return 'Storage connection verified'
    if (typeof res.objectCount === 'number') {
      return `${res.message} (${res.objectCount} blobs, ${formatBytes(res.totalBytes || 0)})`
    }
    return res.message
  }

  async function handleTestSaved(id) {
    setTestingId(id)
    try {
      const res = await api.testStorageProvider(id)
      notify('success', formatTestToast(res))
    } catch (err) {
      notify('error', `Connection test failed: ${err.message}`)
    } finally {
      setTestingId(null)
    }
  }

  async function handleTestForm() {
    setTestingForm(true)
    try {
      const res = await api.testUnsavedStorageProvider(form)
      notify('success', formatTestToast(res))
    } catch (err) {
      notify('error', `Connection failed: ${err.message}`)
    } finally {
      setTestingForm(false)
    }
  }

  async function handleSave(e) {
    e.preventDefault()
    setSaving(true)
    try {
      if (editing) {
        await api.updateStorageProvider(editing.id, form)
        notify('success', `Updated "${form.name}"`)
      } else {
        await api.createStorageProvider(form)
        notify('success', `Added storage destination "${form.name}"`)
      }
      setModalOpen(false)
      onRefresh()
    } catch (err) {
      notify('error', err.message)
    } finally {
      setSaving(false)
    }
  }

  async function handleDelete(provider) {
    if (!window.confirm(`Delete storage provider "${provider.name}"?`)) return
    try {
      await api.deleteStorageProvider(provider.id)
      notify('success', `Deleted "${provider.name}"`)
      onRefresh()
    } catch (err) {
      notify('error', err.message)
    }
  }

  async function handleScan(provider) {
    setScanProvider(provider)
    setScannedObjects([])
    setSelectedImportPaths([])
    setScanLoading(true)
    try {
      const res = await api.scanStorageProvider(provider.id)
      const objs = res.objects || []
      setScannedObjects(objs)
      setSelectedImportPaths(objs.filter((o) => !o.alreadyImported).map((o) => o.storagePath))
    } catch (err) {
      notify('error', `Scan failed: ${err.message}`)
    } finally {
      setScanLoading(false)
    }
  }

  async function handleImportSelected() {
    if (!scanProvider || selectedImportPaths.length === 0) return
    setImporting(true)
    try {
      const res = await api.importStorageBackups(scanProvider.id, selectedImportPaths)
      notify('success', `Imported ${res.imported} backup archive(s) into History`)
      setScanProvider(null)
      onRefresh()
    } catch (err) {
      notify('error', `Import failed: ${err.message}`)
    } finally {
      setImporting(false)
    }
  }

  async function handleSaveAuth(e) {
    e.preventDefault()
    setSavingAuth(true)
    try {
      let res
      if (!authStatus?.hasUsers && authEnabled && authPass) {
        res = await api.setupAuth({ username: authUser, password: authPass })
        if (proxyHeader) {
          res = await api.updateAuthSettings({ trustedProxyHeader: proxyHeader })
        }
      } else {
        res = await api.updateAuthSettings({
          enabled: authEnabled,
          username: authUser,
          newPassword: authPass,
          trustedProxyHeader: proxyHeader,
        })
      }
      setAuthPass('')
      onAuthChange?.(res)
      notify('success', 'Authentication settings saved')
    } catch (err) {
      notify('error', err.message)
    } finally {
      setSavingAuth(false)
    }
  }

  async function refreshNotifications() {
    const res = await api.getNotifications()
    const list = Array.isArray(res?.notifications) ? res.notifications : []
    setNotifList(list)
    onNotificationsChange?.(list)
  }

  function openCreateNotif() {
    setEditingNotif(null)
    setNotifForm(EMPTY_NOTIF_FORM)
    setNotifModalOpen(true)
  }

  function openEditNotif(item) {
    setEditingNotif(item)
    setNotifForm({
      name: item.name || '',
      enabled: Boolean(item.enabled),
      provider: item.provider || 'discord',
      webhookUrl: item.webhookUrl || '',
      telegramBotToken: '',
      hasTelegramBotToken: Boolean(item.hasTelegramBotToken),
      telegramChatId: item.telegramChatId || '',
      telegramThreadId: item.telegramThreadId || '',
      onSuccess: Boolean(item.onSuccess),
      onFailure: item.onFailure ?? true,
    })
    setNotifModalOpen(true)
  }

  async function handleSaveNotifModal(e) {
    e.preventDefault()
    setSavingNotif(true)
    try {
      if (editingNotif) {
        await api.updateNotification(editingNotif.id, notifForm)
        notify('success', `Updated notification channel "${notifForm.name || notifForm.provider}"`)
      } else {
        await api.createNotification(notifForm)
        notify('success', `Added notification channel "${notifForm.name || notifForm.provider}"`)
      }
      await refreshNotifications()
      setNotifModalOpen(false)
    } catch (err) {
      notify('error', err.message)
    } finally {
      setSavingNotif(false)
    }
  }

  async function handleToggleNotif(item) {
    try {
      const res = await api.toggleNotification(item.id)
      await refreshNotifications()
      const stateLabel = res?.notification?.enabled ? 'activated' : 'paused'
      notify('success', `${item.name || item.provider} ${stateLabel}`)
    } catch (err) {
      notify('error', err.message)
    }
  }

  async function handleTestSavedNotif(item) {
    setTestingNotifId(item.id)
    try {
      await api.testNotification(item.id)
      notify('success', `Test alert delivered to ${item.name || item.provider}`)
    } catch (err) {
      notify('error', `Test alert failed: ${err.message}`)
    } finally {
      setTestingNotifId(null)
    }
  }

  async function handleTestModalNotif() {
    setTestingNotifModal(true)
    try {
      await api.testNotifications({
        ...notifForm,
        id: editingNotif?.id || 0,
      })
      notify('success', 'Test alert delivered')
    } catch (err) {
      notify('error', `Test alert failed: ${err.message}`)
    } finally {
      setTestingNotifModal(false)
    }
  }

  async function handleDeleteNotif(item) {
    const label = item.name || item.provider
    if (!window.confirm(`Delete notification channel "${label}"?`)) return
    try {
      await api.deleteNotification(item.id)
      await refreshNotifications()
      notify('success', `Deleted "${label}"`)
    } catch (err) {
      notify('error', err.message)
    }
  }

  function renderProviderSubtitle(p) {
    if (p.type === 'local') {
      return <p className="mt-1 font-mono text-xs text-zinc-400">{p.localPath}</p>
    }
    if (p.type === 'azure') {
      const acct = p.azureAccountName || (p.endpoint ? p.endpoint : 'connection-string')
      return (
        <div className="mt-1 space-y-1 text-xs text-zinc-400">
          <p className="font-mono">
            azure://{acct}/{p.azureContainer}
            {p.prefix ? `/${p.prefix}` : ''}
          </p>
          <div className="flex flex-wrap items-center gap-1.5">
            <Badge variant="info">Tier: {p.azureAccessTier || 'Cool'}</Badge>
            <Badge variant="neutral">
              Auth:{' '}
              {p.azureAuthType === 'service_principal'
                ? 'Service Principal'
                : p.azureAuthType === 'connection_string'
                  ? 'Connection String'
                  : (p.azureAuthType || 'key').toUpperCase()}
            </Badge>
            {p.azureAutoCreateContainer && <Badge variant="neutral">Auto-create</Badge>}
          </div>
        </div>
      )
    }
    return (
      <div className="mt-1 space-y-0.5 text-xs text-zinc-400">
        <p className="font-mono">
          {p.useSSL ? 'https://' : 'http://'}
          {p.endpoint}/{p.bucket}
          {p.prefix ? `/${p.prefix}` : ''}
        </p>
        <p>
          Region: {p.region || 'auto'} · Key: {p.accessKey}
        </p>
      </div>
    )
  }

  function renderProviderIcon(type) {
    if (type === 's3') return <Cloud className="h-5 w-5" />
    if (type === 'azure') return <Container className="h-5 w-5" />
    return <HardDrive className="h-5 w-5" />
  }

  const notifOverview = summarizeNotificationsList(notifList)

  return (
    <div className="grid grid-cols-1 lg:grid-cols-12 gap-6 items-start">
      {/* Settings Sidebar Navigation */}
      <Card className="lg:col-span-3 p-2">
        <div className="px-3 py-2 text-[11px] font-semibold uppercase tracking-wider text-zinc-500">
          Settings
        </div>
        <nav className="flex lg:flex-col gap-1 overflow-x-auto">
          {SETTINGS_TABS.map((tab) => {
            const Icon = tab.icon
            const active = section === tab.id
            return (
              <button
                key={tab.id}
                type="button"
                onClick={() => setSection(tab.id)}
                style={
                  active
                    ? {
                        backgroundColor: 'var(--dv-accent-soft)',
                        color: 'var(--dv-accent-text)',
                        borderColor: 'var(--dv-accent-border)',
                      }
                    : undefined
                }
                className={`flex items-center justify-between gap-2 whitespace-nowrap rounded-lg border px-3 py-2 text-xs font-medium transition-colors cursor-pointer ${
                  active ? '' : 'border-transparent text-zinc-400 hover:bg-zinc-900/50 hover:text-zinc-200'
                }`}
              >
                <span className="flex items-center gap-2.5">
                  <Icon className="h-4 w-4 shrink-0" />
                  {tab.label}
                </span>
                {tab.id === 'notifications' && notifOverview.configuredCount > 0 && (
                  <Badge
                    variant={notifOverview.activeCount > 0 ? 'success' : 'warning'}
                    className="px-1.5 py-0 text-[10px]"
                  >
                    {notifOverview.activeCount > 0
                      ? `${notifOverview.activeCount} active`
                      : `${notifOverview.configuredCount} paused`}
                  </Badge>
                )}
              </button>
            )
          })}
        </nav>
      </Card>

      {/* Active Settings Section */}
      <div className="lg:col-span-9 space-y-6">
        {section === 'storage' && (
          <div className="space-y-4">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <h2 className="text-lg font-semibold text-zinc-100">Storage Destinations</h2>
                <p className="text-xs text-zinc-400">
                  Configure local disks, Azure Blob Storage, and S3-compatible buckets with optional AES-256-GCM encryption
                </p>
              </div>
              <Button onClick={openCreate}>
                <Plus className="h-4 w-4" />
                Add Storage Provider
              </Button>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              {providers.map((p) => (
                <Card key={p.id} className="p-5">
                  <div className="flex items-start justify-between gap-3">
                    <div className="flex items-start gap-3">
                      <div
                        style={{ color: 'var(--dv-accent-text)' }}
                        className="rounded-xl border border-zinc-800 bg-zinc-950 p-2.5"
                      >
                        {renderProviderIcon(p.type)}
                      </div>
                      <div>
                        <div className="flex flex-wrap items-center gap-2">
                          <h3 className="text-sm font-semibold text-zinc-100">{p.name}</h3>
                          <Badge variant="neutral">{p.type.toUpperCase()}</Badge>
                          {p.isDefault && <Badge variant="success">Default</Badge>}
                          {p.encryptBackups && (
                            <Badge variant="info">
                              <Lock className="h-3 w-3" />
                              Encrypted
                            </Badge>
                          )}
                        </div>
                        {renderProviderSubtitle(p)}
                      </div>
                    </div>
                  </div>

                  <div className="mt-4 flex flex-wrap items-center justify-end gap-2 border-t border-zinc-800/70 pt-3">
                    <Button
                      variant="outline"
                      size="sm"
                      title="Scan destination for existing backups to import"
                      onClick={() => handleScan(p)}
                    >
                      <FolderSearch className="h-3.5 w-3.5" />
                      Scan &amp; Import
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={testingId === p.id}
                      onClick={() => handleTestSaved(p.id)}
                    >
                      <PlugZap className="h-3.5 w-3.5" />
                      {testingId === p.id ? 'Testing...' : 'Test'}
                    </Button>
                    <Button variant="ghost" size="sm" onClick={() => openEdit(p)}>
                      <Edit2 className="h-3.5 w-3.5" />
                      Edit
                    </Button>
                    <Button variant="danger" size="sm" onClick={() => handleDelete(p)}>
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </Card>
              ))}
            </div>
          </div>
        )}

        {section === 'appearance' && (
          <Card className="p-5">
            <div className="flex flex-wrap items-center justify-between gap-3 mb-5">
              <div>
                <h2 className="text-base font-semibold text-zinc-100">Appearance &amp; Workspace UI</h2>
                <p className="text-xs text-zinc-400">
                  Customize theme, accent color, layout density, and default behaviors (saved in SQLite)
                </p>
              </div>
              <Button variant="outline" size="sm" onClick={handleResetPreferences} disabled={savingPrefs}>
                <RotateCcw className="h-3.5 w-3.5" />
                Reset Defaults
              </Button>
            </div>

            <form onSubmit={handleSavePreferences} className="space-y-5">
              {/* Theme & Accent */}
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div>
                  <Label>Theme Mode</Label>
                  <div className="grid grid-cols-3 gap-2">
                    {[
                      { id: 'dark', label: 'Dark', icon: Moon },
                      { id: 'light', label: 'Light', icon: Sun },
                      { id: 'system', label: 'System', icon: Monitor },
                    ].map((opt) => {
                      const Icon = opt.icon
                      const active = prefsDraft.theme === opt.id
                      return (
                        <button
                          key={opt.id}
                          type="button"
                          onClick={() => updatePrefPreview({ theme: opt.id })}
                          style={
                            active
                              ? {
                                  borderColor: 'var(--dv-accent)',
                                  backgroundColor: 'var(--dv-accent-soft)',
                                  color: 'var(--dv-accent-text)',
                                }
                              : { borderColor: 'var(--dv-border-strong)' }
                          }
                          className="flex items-center justify-center gap-1.5 rounded-lg border px-3 py-2 text-xs font-medium cursor-pointer"
                        >
                          <Icon className="h-3.5 w-3.5" />
                          {opt.label}
                        </button>
                      )
                    })}
                  </div>
                </div>

                <div>
                  <Label>Accent Color</Label>
                  <div className="flex items-center gap-2.5 pt-1">
                    {ACCENT_OPTIONS.map((opt) => {
                      const active = prefsDraft.accentColor === opt.id
                      return (
                        <button
                          key={opt.id}
                          type="button"
                          title={opt.label}
                          onClick={() => updatePrefPreview({ accentColor: opt.id })}
                          style={{ backgroundColor: opt.swatch }}
                          className={`h-8 w-8 rounded-full transition-transform cursor-pointer ${
                            active ? 'ring-2 ring-white ring-offset-2 ring-offset-zinc-950 scale-110' : 'opacity-80 hover:opacity-100'
                          }`}
                        />
                      )
                    })}
                  </div>
                </div>
              </div>

              {/* Instance Name & Density */}
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                <div>
                  <Label htmlFor="pref-instance">Instance Badge / Host Label</Label>
                  <Input
                    id="pref-instance"
                    value={prefsDraft.instanceName || ''}
                    onChange={(e) => updatePrefPreview({ instanceName: e.target.value })}
                    placeholder="e.g. prod-eu or nas-01"
                  />
                </div>
                <div>
                  <Label htmlFor="pref-density">Table &amp; Layout Density</Label>
                  <Select
                    id="pref-density"
                    value={prefsDraft.density || 'comfortable'}
                    onChange={(e) => updatePrefPreview({ density: e.target.value })}
                  >
                    <option value="comfortable">Comfortable</option>
                    <option value="compact">Compact</option>
                  </Select>
                </div>
                <div>
                  <Label htmlFor="pref-time">Clock Format</Label>
                  <Select
                    id="pref-time"
                    value={prefsDraft.timeFormat || '24h'}
                    onChange={(e) => updatePrefPreview({ timeFormat: e.target.value })}
                  >
                    <option value="24h">24-hour (14:30:00)</option>
                    <option value="12h">12-hour (02:30:00 PM)</option>
                  </Select>
                </div>
              </div>

              {/* Navigation & Refresh Defaults */}
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                <div>
                  <Label htmlFor="pref-page">Default Landing Page</Label>
                  <Select
                    id="pref-page"
                    value={prefsDraft.defaultPage || 'dashboard'}
                    onChange={(e) => updatePrefPreview({ defaultPage: e.target.value })}
                  >
                    <option value="dashboard">Dashboard</option>
                    <option value="containers">Containers &amp; Volumes</option>
                    <option value="schedules">Schedules</option>
                    <option value="history">History</option>
                  </Select>
                </div>
                <div>
                  <Label htmlFor="pref-tab">Default Containers Tab</Label>
                  <Select
                    id="pref-tab"
                    value={prefsDraft.defaultResourceTab || 'stacks'}
                    onChange={(e) => updatePrefPreview({ defaultResourceTab: e.target.value })}
                  >
                    <option value="stacks">Compose Stacks</option>
                    <option value="containers">Containers</option>
                    <option value="volumes">Volumes</option>
                  </Select>
                </div>
                <div>
                  <Label htmlFor="pref-refresh">Background Auto-Refresh</Label>
                  <Select
                    id="pref-refresh"
                    value={String(prefsDraft.autoRefreshSeconds ?? 0)}
                    onChange={(e) => updatePrefPreview({ autoRefreshSeconds: Number(e.target.value) })}
                  >
                    <option value="0">Off (manual / live backup poll only)</option>
                    <option value="10">Every 10 seconds</option>
                    <option value="30">Every 30 seconds</option>
                    <option value="60">Every 60 seconds</option>
                  </Select>
                </div>
              </div>

              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <div className="flex items-center justify-between rounded-xl border border-zinc-800 bg-zinc-950/50 px-3.5 py-2.5">
                  <span className="text-xs text-zinc-200">Show stopped containers by default</span>
                  <Switch
                    checked={Boolean(prefsDraft.showStoppedDefault)}
                    onChange={(val) => updatePrefPreview({ showStoppedDefault: val })}
                  />
                </div>
                <div className="flex items-center justify-between rounded-xl border border-zinc-800 bg-zinc-950/50 px-3.5 py-2.5">
                  <span className="text-xs text-zinc-200">Pause containers by default in modals</span>
                  <Switch
                    checked={Boolean(prefsDraft.defaultPauseBackups)}
                    onChange={(val) => updatePrefPreview({ defaultPauseBackups: val })}
                  />
                </div>
              </div>

              <div className="flex items-center justify-end">
                <Button type="submit" disabled={savingPrefs}>
                  {savingPrefs ? 'Saving...' : 'Save Appearance & UI Settings'}
                </Button>
              </div>
            </form>
          </Card>
        )}

        {section === 'security' && (
          <Card className="p-5">
            <div className="flex items-center justify-between mb-4">
              <div className="flex items-center gap-2">
                <ShieldCheck className="h-4 w-4 text-emerald-400" />
                <div>
                  <h3 className="text-base font-semibold text-zinc-100">Security &amp; Authentication</h3>
                  <p className="text-xs text-zinc-400">
                    Protect DockVault&apos;s Docker socket and cloud keys with built-in login or a trusted reverse-proxy header
                  </p>
                </div>
              </div>
              <Switch checked={authEnabled} onChange={setAuthEnabled} />
            </div>

            <form onSubmit={handleSaveAuth} className="space-y-4">
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                <div>
                  <Label htmlFor="auth-user">Admin Username</Label>
                  <Input
                    id="auth-user"
                    value={authUser}
                    onChange={(e) => setAuthUser(e.target.value)}
                    placeholder="admin"
                  />
                </div>
                <div>
                  <Label htmlFor="auth-pass">
                    {authStatus?.hasUsers ? 'New Password (leave blank to keep)' : 'Admin Password (min 6 chars)'}
                  </Label>
                  <Input
                    id="auth-pass"
                    type="password"
                    value={authPass}
                    onChange={(e) => setAuthPass(e.target.value)}
                    placeholder="••••••••"
                  />
                </div>
                <div>
                  <Label htmlFor="auth-proxy">Trusted Proxy Header (optional)</Label>
                  <Input
                    id="auth-proxy"
                    value={proxyHeader}
                    onChange={(e) => setProxyHeader(e.target.value)}
                    placeholder="Remote-User or X-Forwarded-User"
                    className="font-mono"
                  />
                </div>
              </div>
              <div className="flex items-center justify-end">
                <Button type="submit" size="sm" disabled={savingAuth}>
                  {savingAuth ? 'Saving...' : 'Save Security Settings'}
                </Button>
              </div>
            </form>
          </Card>
        )}

        {section === 'notifications' && (
          <div className="space-y-4">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <div className="flex flex-wrap items-center gap-2">
                  <h2 className="text-lg font-semibold text-zinc-100">Notifications &amp; Alerts</h2>
                  {notifOverview.configuredCount > 0 && (
                    <Badge variant={notifOverview.activeCount > 0 ? 'success' : 'warning'}>
                      {notifOverview.activeCount} active · {notifOverview.configuredCount} configured
                    </Badge>
                  )}
                </div>
                <p className="text-xs text-zinc-400">
                  Configure multiple Telegram, Discord, Slack, ntfy, or JSON webhook destinations and activate them independently
                </p>
              </div>
              <Button onClick={openCreateNotif}>
                <Plus className="h-4 w-4" />
                Add Channel
              </Button>
            </div>

            {notifOverview.configuredCount === 0 ? (
              <EmptyState
                icon={Bell}
                title="No notification channels configured"
                description="Add one or more channels (Telegram, Discord, Slack, ntfy, or webhook) to receive alerts when backups finish or fail."
                action={
                  <Button size="sm" onClick={openCreateNotif}>
                    <Plus className="h-4 w-4" />
                    Add Notification Channel
                  </Button>
                }
              />
            ) : (
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                {notifOverview.channels.map((ch) => (
                  <Card key={ch.id} className="p-5 flex flex-col justify-between gap-4">
                    <div className="flex items-start justify-between gap-3">
                      <div className="flex items-start gap-3 min-w-0">
                        <div
                          className={`rounded-xl border p-2.5 shrink-0 ${
                            ch.summary.active
                              ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-400'
                              : 'border-zinc-800 bg-zinc-950 text-zinc-400'
                          }`}
                        >
                          {ch.summary.active ? (
                            <Bell className="h-5 w-5" />
                          ) : (
                            <BellOff className="h-5 w-5" />
                          )}
                        </div>
                        <div className="min-w-0">
                          <div className="flex flex-wrap items-center gap-1.5">
                            <h3 className="text-sm font-semibold text-zinc-100 truncate">
                              {ch.displayName}
                            </h3>
                            <Badge variant={ch.summary.active ? 'success' : 'warning'}>
                              {ch.summary.active ? 'Active' : 'Paused'}
                            </Badge>
                            <Badge variant="neutral">{ch.summary.providerLabel}</Badge>
                          </div>

                          <p className="mt-1 truncate font-mono text-xs text-zinc-300">
                            {ch.summary.targetSummary || 'No target'}
                          </p>

                          <div className="mt-2 flex flex-wrap items-center gap-1.5 text-[11px] text-zinc-400">
                            <Badge variant="info" className="text-[10px] py-0 px-1.5">
                              Events: {ch.summary.triggers}
                            </Badge>
                            {ch.provider === 'telegram' && ch.hasTelegramBotToken && (
                              <Badge variant="success" className="text-[10px] py-0 px-1.5">
                                Token Saved
                              </Badge>
                            )}
                            {ch.updatedAt && (
                              <span className="text-[11px] text-zinc-500">
                                Saved {formatDateTime(ch.updatedAt)}
                              </span>
                            )}
                          </div>
                        </div>
                      </div>

                      <div className="shrink-0 pt-0.5" title={ch.enabled ? 'Disable channel' : 'Activate channel'}>
                        <Switch checked={Boolean(ch.enabled)} onChange={() => handleToggleNotif(ch)} />
                      </div>
                    </div>

                    <div className="flex flex-wrap items-center justify-end gap-2 border-t border-zinc-800/80 pt-3">
                      <Button
                        variant="outline"
                        size="sm"
                        disabled={testingNotifId === ch.id}
                        onClick={() => handleTestSavedNotif(ch)}
                      >
                        <Send className="h-3.5 w-3.5" />
                        {testingNotifId === ch.id ? 'Sending...' : 'Test'}
                      </Button>
                      <Button variant="ghost" size="sm" onClick={() => openEditNotif(ch)}>
                        <Edit2 className="h-3.5 w-3.5" />
                        Edit
                      </Button>
                      <Button variant="danger" size="sm" onClick={() => handleDeleteNotif(ch)}>
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  </Card>
                ))}
              </div>
            )}
          </div>
        )}

        {section === 'system' && (
          <Card className="p-5 space-y-4">
            <div className="flex items-center gap-2">
              <Server className="h-4 w-4 text-zinc-400" />
              <h3 className="text-base font-semibold text-zinc-100">Runtime Configuration</h3>
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 text-xs">
              <div className="rounded-xl border border-zinc-800 bg-zinc-950/60 p-3">
                <div className="text-zinc-500">DockVault Version</div>
                <div className="mt-1 font-mono text-zinc-200">{dashboard?.version || 'dev'}</div>
              </div>
              <div className="rounded-xl border border-zinc-800 bg-zinc-950/60 p-3">
                <div className="text-zinc-500">Archive Helper Image</div>
                <div className="mt-1 font-mono text-zinc-200">{dashboard?.helperImage || 'alpine:3.21'}</div>
              </div>
              <div className="rounded-xl border border-zinc-800 bg-zinc-950/60 p-3">
                <div className="text-zinc-500">Default Backup Directory</div>
                <div className="mt-1 truncate font-mono text-zinc-200">{dashboard?.backupDir || '/app/backups'}</div>
              </div>
            </div>
            <div className="rounded-xl border border-zinc-800 bg-zinc-950/60 p-3 text-xs text-zinc-400">
              Tip: Press <kbd className="rounded border border-zinc-700 px-1.5 py-0.5 font-mono text-[11px]">R</kbd> anywhere outside text fields to refresh containers, schedules, and history.
            </div>
          </Card>
        )}
      </div>

      {/* Add / Edit Storage Modal */}
      <Modal
        open={modalOpen}
        onClose={() => setModalOpen(false)}
        title={editing ? 'Edit Storage Provider' : 'Add Storage Provider'}
        subtitle="Configure local disk, Azure Blob Storage, or S3-compatible object storage"
        maxWidth="max-w-xl"
      >
        <form onSubmit={handleSave} className="space-y-4 max-h-[80vh] overflow-y-auto pr-1">
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
            <div className="sm:col-span-2">
              <Label htmlFor="sp-name">Display Name</Label>
              <Input
                id="sp-name"
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder={form.type === 'azure' ? 'Azure Offsite Vault' : 'MinIO NAS'}
                required
              />
            </div>
            <div>
              <Label htmlFor="sp-type">Type</Label>
              <Select
                id="sp-type"
                value={form.type}
                disabled={Boolean(editing)}
                onChange={(e) => setForm({ ...form, type: e.target.value })}
              >
                <option value="s3">S3 Compatible</option>
                <option value="azure">Azure Blob Storage</option>
                <option value="local">Local Disk</option>
              </Select>
            </div>
          </div>

          {form.type === 'local' && (
            <div>
              <Label htmlFor="sp-localpath">Directory Path</Label>
              <Input
                id="sp-localpath"
                value={form.localPath}
                onChange={(e) => setForm({ ...form, localPath: e.target.value })}
                placeholder="/app/backups"
                className="font-mono"
                required
              />
            </div>
          )}

          {form.type === 's3' && (
            <div className="space-y-3">
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                <div className="sm:col-span-2">
                  <Label htmlFor="sp-endpoint">Endpoint</Label>
                  <Input
                    id="sp-endpoint"
                    value={form.endpoint}
                    onChange={(e) => setForm({ ...form, endpoint: e.target.value })}
                    placeholder="s3.amazonaws.com or minio.local:9000"
                    className="font-mono"
                    required
                  />
                </div>
                <div>
                  <Label htmlFor="sp-region">Region</Label>
                  <Input
                    id="sp-region"
                    value={form.region}
                    onChange={(e) => setForm({ ...form, region: e.target.value })}
                    placeholder="us-east-1"
                  />
                </div>
              </div>

              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <div>
                  <Label htmlFor="sp-bucket">Bucket Name</Label>
                  <Input
                    id="sp-bucket"
                    value={form.bucket}
                    onChange={(e) => setForm({ ...form, bucket: e.target.value })}
                    placeholder="dockvault-backups"
                    required
                  />
                </div>
                <div>
                  <Label htmlFor="sp-prefix">Key Prefix (optional)</Label>
                  <Input
                    id="sp-prefix"
                    value={form.prefix}
                    onChange={(e) => setForm({ ...form, prefix: e.target.value })}
                    placeholder="homelab"
                  />
                </div>
              </div>

              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <div>
                  <Label htmlFor="sp-access">Access Key</Label>
                  <Input
                    id="sp-access"
                    value={form.accessKey}
                    onChange={(e) => setForm({ ...form, accessKey: e.target.value })}
                    required
                  />
                </div>
                <div>
                  <Label htmlFor="sp-secret">
                    Secret Key {editing?.hasSecretKey ? '(leave blank to keep)' : ''}
                  </Label>
                  <Input
                    id="sp-secret"
                    type="password"
                    value={form.secretKey}
                    onChange={(e) => setForm({ ...form, secretKey: e.target.value })}
                    required={!editing}
                  />
                </div>
              </div>

              <div className="flex items-center justify-between rounded-xl border border-zinc-800 bg-zinc-950/50 px-3.5 py-2.5">
                <span className="text-xs text-zinc-200">Use TLS / HTTPS</span>
                <Switch
                  checked={form.useSSL}
                  onChange={(val) => setForm({ ...form, useSSL: val })}
                />
              </div>
            </div>
          )}

          {form.type === 'azure' && (
            <div className="space-y-3">
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <div>
                  <Label htmlFor="az-auth">Authentication Method</Label>
                  <Select
                    id="az-auth"
                    value={form.azureAuthType}
                    onChange={(e) => setForm({ ...form, azureAuthType: e.target.value })}
                  >
                    <option value="key">Account Name &amp; Shared Key</option>
                    <option value="service_principal">Service Principal / App Registration</option>
                    <option value="connection_string">Connection String</option>
                    <option value="sas">SAS Token (Shared Access Signature)</option>
                  </Select>
                </div>
                <div>
                  <Label htmlFor="az-tier">Blob Access Tier</Label>
                  <Select
                    id="az-tier"
                    value={form.azureAccessTier}
                    onChange={(e) => setForm({ ...form, azureAccessTier: e.target.value })}
                  >
                    {AZURE_TIERS.map((t) => (
                      <option key={t.value} value={t.value}>
                        {t.label}
                      </option>
                    ))}
                  </Select>
                </div>
              </div>

              {form.azureAuthType !== 'connection_string' && (
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                  <div>
                    <Label htmlFor="az-account">Storage Account Name</Label>
                    <Input
                      id="az-account"
                      value={form.azureAccountName}
                      onChange={(e) => setForm({ ...form, azureAccountName: e.target.value })}
                      placeholder="mystorageaccount"
                      className="font-mono"
                      required={
                        form.azureAuthType === 'key' || form.azureAuthType === 'service_principal'
                      }
                    />
                  </div>
                  <div>
                    <Label htmlFor="az-endpoint">Custom Endpoint (optional / Azurite)</Label>
                    <Input
                      id="az-endpoint"
                      value={form.endpoint}
                      onChange={(e) => setForm({ ...form, endpoint: e.target.value })}
                      placeholder="http://azurite:10000/devstoreaccount1"
                      className="font-mono"
                    />
                  </div>
                </div>
              )}

              {form.azureAuthType === 'service_principal' && (
                <div className="space-y-2 rounded-xl border border-zinc-800 bg-zinc-950/50 p-3">
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                    <div>
                      <Label htmlFor="az-tenant">Directory (Tenant) ID</Label>
                      <Input
                        id="az-tenant"
                        value={form.azureTenantId}
                        onChange={(e) => setForm({ ...form, azureTenantId: e.target.value })}
                        placeholder="00000000-0000-0000-0000-000000000000"
                        className="font-mono"
                        required
                      />
                    </div>
                    <div>
                      <Label htmlFor="az-client">Application (Client) ID</Label>
                      <Input
                        id="az-client"
                        value={form.azureClientId}
                        onChange={(e) => setForm({ ...form, azureClientId: e.target.value })}
                        placeholder="00000000-0000-0000-0000-000000000000"
                        className="font-mono"
                        required
                      />
                    </div>
                  </div>
                  <p className="text-[11px] text-zinc-400">
                    Assign <span className="font-mono text-zinc-300">Storage Blob Data Contributor</span> on the storage account/container (plus{' '}
                    <span className="font-mono text-zinc-300">Storage Blob Delegator</span> for 1h User Delegation SAS links).
                  </p>
                </div>
              )}

              <div>
                <Label htmlFor="az-secret">
                  {form.azureAuthType === 'connection_string'
                    ? 'Connection String'
                    : form.azureAuthType === 'sas'
                      ? 'SAS Token (?sv=...)'
                      : form.azureAuthType === 'service_principal'
                        ? 'Client Secret'
                        : 'Storage Account Key'}{' '}
                  {editing?.hasSecretKey ? '(leave blank to keep)' : ''}
                </Label>
                <Input
                  id="az-secret"
                  type="password"
                  value={form.secretKey}
                  onChange={(e) => setForm({ ...form, secretKey: e.target.value })}
                  placeholder={
                    form.azureAuthType === 'connection_string'
                      ? 'DefaultEndpointsProtocol=https;AccountName=...;AccountKey=...'
                      : form.azureAuthType === 'sas'
                        ? 'sv=2024-08-04&ss=b&srt=co&sp=rwdlac...'
                        : form.azureAuthType === 'service_principal'
                          ? 'App Registration client secret value'
                          : 'Base64 account access key'
                  }
                  className="font-mono"
                  required={!editing}
                />
              </div>

              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <div>
                  <Label htmlFor="az-container">Blob Container Name</Label>
                  <Input
                    id="az-container"
                    value={form.azureContainer}
                    onChange={(e) => setForm({ ...form, azureContainer: e.target.value })}
                    placeholder="dockvault-backups"
                    className="font-mono"
                    required
                  />
                </div>
                <div>
                  <Label htmlFor="az-prefix">Blob Prefix (optional)</Label>
                  <Input
                    id="az-prefix"
                    value={form.prefix}
                    onChange={(e) => setForm({ ...form, prefix: e.target.value })}
                    placeholder="homelab"
                    className="font-mono"
                  />
                </div>
              </div>

              <div className="flex items-center justify-between rounded-xl border border-zinc-800 bg-zinc-950/50 px-3.5 py-2.5">
                <div>
                  <div className="text-xs text-zinc-200">Auto-create blob container if missing</div>
                  <p className="text-[11px] text-zinc-400">
                    Automatically creates the container on test or first backup.
                  </p>
                </div>
                <Switch
                  checked={form.azureAutoCreateContainer}
                  onChange={(val) => setForm({ ...form, azureAutoCreateContainer: val })}
                />
              </div>
            </div>
          )}

          {/* Client-Side Archive Encryption */}
          <div className="rounded-xl border border-zinc-800 bg-zinc-950/50 p-3 space-y-3">
            <div className="flex items-center justify-between">
              <div>
                <div className="flex items-center gap-1.5 text-xs font-medium text-zinc-200">
                  <Lock className="h-3.5 w-3.5 text-sky-400" />
                  Client-Side Encryption (AES-256-GCM)
                </div>
                <p className="text-[11px] text-zinc-400">
                  Encrypts archives into <span className="font-mono">.tar.gz.enc</span> before upload; transparently decrypted on inspect, download, and restore.
                </p>
              </div>
              <Switch
                checked={form.encryptBackups}
                onChange={(val) => setForm({ ...form, encryptBackups: val })}
              />
            </div>

            {form.encryptBackups && (
              <div>
                <Label htmlFor="sp-enc-pass">
                  Encryption Passphrase {editing?.hasEncryptionKey ? '(leave blank to keep)' : ''}
                </Label>
                <Input
                  id="sp-enc-pass"
                  type="password"
                  value={form.encryptionPassphrase}
                  onChange={(e) => setForm({ ...form, encryptionPassphrase: e.target.value })}
                  placeholder="Strong passphrase (or uses DOCKVAULT_ENCRYPTION_KEY env if set)"
                  className="font-mono"
                />
              </div>
            )}
          </div>

          <div className="flex items-center justify-between rounded-xl border border-zinc-800 bg-zinc-950/50 px-3.5 py-2.5">
            <span className="text-xs text-zinc-200">Set as default storage destination</span>
            <Switch
              checked={form.isDefault}
              onChange={(val) => setForm({ ...form, isDefault: val })}
            />
          </div>

          <div className="flex items-center justify-between gap-2 pt-2">
            {(!editing || form.secretKey || form.type === 'local') ? (
              <Button
                type="button"
                variant="outline"
                onClick={handleTestForm}
                disabled={testingForm}
              >
                <PlugZap className="h-4 w-4" />
                {testingForm ? 'Testing...' : 'Test Connection'}
              </Button>
            ) : (
              <span />
            )}

            <div className="flex items-center gap-2">
              <Button type="button" variant="ghost" onClick={() => setModalOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={saving}>
                {saving ? 'Saving...' : editing ? 'Save Changes' : 'Add Provider'}
              </Button>
            </div>
          </div>
        </form>
      </Modal>

      {/* Disaster Recovery: Scan & Import Modal */}
      <Modal
        open={Boolean(scanProvider)}
        onClose={() => setScanProvider(null)}
        title="Scan & Import Remote Backups"
        subtitle={scanProvider ? `Destination: ${scanProvider.name} (${scanProvider.type.toUpperCase()})` : ''}
        maxWidth="max-w-2xl"
      >
        <div className="space-y-4">
          <p className="text-xs text-zinc-400">
            Discover existing <span className="font-mono">.tar.gz</span> and{' '}
            <span className="font-mono">.tar.gz.enc</span> archives on this storage destination and import them into History for disaster recovery.
          </p>

          <div className="max-h-72 overflow-y-auto rounded-xl border border-zinc-800 bg-zinc-950 p-2">
            {scanLoading ? (
              <div className="py-10 text-center text-xs text-zinc-400">
                Scanning storage destination...
              </div>
            ) : scannedObjects.length === 0 ? (
              <div className="py-10 text-center text-xs text-zinc-500">
                No backup archives found at this destination.
              </div>
            ) : (
              <div className="space-y-1">
                {scannedObjects.map((obj) => {
                  const checked = selectedImportPaths.includes(obj.storagePath)
                  return (
                    <label
                      key={obj.storagePath}
                      className={`flex items-center justify-between gap-3 rounded-lg px-3 py-2 text-xs ${
                        obj.alreadyImported
                          ? 'opacity-50'
                          : 'hover:bg-zinc-900 cursor-pointer'
                      }`}
                    >
                      <span className="flex items-center gap-2.5 min-w-0">
                        <input
                          type="checkbox"
                          disabled={obj.alreadyImported}
                          checked={checked}
                          onChange={() =>
                            setSelectedImportPaths((prev) =>
                              prev.includes(obj.storagePath)
                                ? prev.filter((p) => p !== obj.storagePath)
                                : [...prev, obj.storagePath],
                            )
                          }
                          className="accent-sky-500"
                        />
                        <span className="truncate font-mono text-zinc-200">{obj.name}</span>
                        {obj.encrypted && <Badge variant="info">Encrypted</Badge>}
                        {obj.alreadyImported && <Badge variant="success">Imported</Badge>}
                      </span>
                      <span className="shrink-0 text-[11px] text-zinc-400">
                        {formatBytes(obj.sizeBytes)} · {formatDateTime(obj.lastModified)}
                      </span>
                    </label>
                  )
                })}
              </div>
            )}
          </div>

          <div className="flex items-center justify-between pt-2">
            <span className="text-xs text-zinc-400">
              {selectedImportPaths.length} selected for import
            </span>
            <div className="flex items-center gap-2">
              <Button type="button" variant="ghost" onClick={() => setScanProvider(null)}>
                Close
              </Button>
              <Button
                type="button"
                disabled={importing || selectedImportPaths.length === 0}
                onClick={handleImportSelected}
              >
                <DownloadCloud className="h-4 w-4" />
                {importing ? 'Importing...' : `Import (${selectedImportPaths.length})`}
              </Button>
            </div>
          </div>
        </div>
      </Modal>

      {/* Add / Edit Notification Channel Modal */}
      <Modal
        open={notifModalOpen}
        onClose={() => setNotifModalOpen(false)}
        title={editingNotif ? 'Edit Notification Channel' : 'Add Notification Channel'}
        subtitle="Configure alerts for Telegram, Discord, Slack, ntfy, or any HTTP webhook"
        maxWidth="max-w-lg"
      >
        <form onSubmit={handleSaveNotifModal} className="space-y-4">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div>
              <Label htmlFor="notif-modal-name">Channel Name</Label>
              <Input
                id="notif-modal-name"
                value={notifForm.name}
                onChange={(e) => setNotifForm({ ...notifForm, name: e.target.value })}
                placeholder={
                  notifForm.provider === 'telegram' ? 'Homelab Telegram' : 'Ops Webhook'
                }
              />
            </div>
            <div>
              <Label htmlFor="notif-modal-provider">Provider</Label>
              <Select
                id="notif-modal-provider"
                value={notifForm.provider}
                onChange={(e) => setNotifForm({ ...notifForm, provider: e.target.value })}
              >
                <option value="telegram">Telegram Bot</option>
                <option value="discord">Discord Webhook</option>
                <option value="slack">Slack Webhook</option>
                <option value="ntfy">ntfy Topic URL</option>
                <option value="generic">Generic JSON Webhook</option>
              </Select>
            </div>
          </div>

          {notifForm.provider !== 'telegram' ? (
            <div>
              <Label htmlFor="notif-modal-url">Webhook / Topic URL</Label>
              <Input
                id="notif-modal-url"
                value={notifForm.webhookUrl}
                onChange={(e) => setNotifForm({ ...notifForm, webhookUrl: e.target.value })}
                placeholder="https://discord.com/api/webhooks/... or https://ntfy.sh/dockvault"
                className="font-mono"
                required
              />
            </div>
          ) : (
            <>
              <div>
                <Label htmlFor="notif-modal-tg-token" className="flex items-center gap-2">
                  <span>Bot Token</span>
                  {notifForm.hasTelegramBotToken && (
                    <Badge variant="success" className="px-1.5 py-0 text-[10px]">
                      Token Saved
                    </Badge>
                  )}
                </Label>
                <Input
                  id="notif-modal-tg-token"
                  type="password"
                  value={notifForm.telegramBotToken}
                  onChange={(e) =>
                    setNotifForm({ ...notifForm, telegramBotToken: e.target.value })
                  }
                  placeholder={
                    notifForm.hasTelegramBotToken
                      ? '•••••••••••••••••••• (token stored — leave blank to keep)'
                      : '123456789:AAH...'
                  }
                  className="font-mono"
                  required={!notifForm.hasTelegramBotToken}
                />
              </div>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <div>
                  <Label htmlFor="notif-modal-tg-chat">Chat ID</Label>
                  <Input
                    id="notif-modal-tg-chat"
                    value={notifForm.telegramChatId}
                    onChange={(e) =>
                      setNotifForm({ ...notifForm, telegramChatId: e.target.value })
                    }
                    placeholder="-1001234567890 or 123456789"
                    className="font-mono"
                    required
                  />
                </div>
                <div>
                  <Label htmlFor="notif-modal-tg-thread">Topic / Thread ID (optional)</Label>
                  <Input
                    id="notif-modal-tg-thread"
                    value={notifForm.telegramThreadId}
                    onChange={(e) =>
                      setNotifForm({ ...notifForm, telegramThreadId: e.target.value })
                    }
                    placeholder="Supergroup topic ID"
                    className="font-mono"
                  />
                </div>
              </div>
            </>
          )}

          <div className="space-y-2">
            <div className="flex items-center justify-between rounded-xl border border-zinc-800 bg-zinc-950/50 px-3.5 py-2.5">
              <span className="text-xs font-medium text-zinc-200">Channel Active</span>
              <Switch
                checked={notifForm.enabled}
                onChange={(val) => setNotifForm({ ...notifForm, enabled: val })}
              />
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
              <div className="flex items-center justify-between rounded-xl border border-zinc-800 bg-zinc-950/50 px-3.5 py-2.5">
                <span className="text-xs text-zinc-200">On backup failure</span>
                <Switch
                  checked={notifForm.onFailure}
                  onChange={(val) => setNotifForm({ ...notifForm, onFailure: val })}
                />
              </div>
              <div className="flex items-center justify-between rounded-xl border border-zinc-800 bg-zinc-950/50 px-3.5 py-2.5">
                <span className="text-xs text-zinc-200">On backup success</span>
                <Switch
                  checked={notifForm.onSuccess}
                  onChange={(val) => setNotifForm({ ...notifForm, onSuccess: val })}
                />
              </div>
            </div>
          </div>

          <div className="flex flex-wrap items-center justify-between gap-2 pt-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={handleTestModalNotif}
              disabled={
                testingNotifModal ||
                (notifForm.provider === 'telegram'
                  ? (!notifForm.telegramBotToken && !notifForm.hasTelegramBotToken) ||
                    !notifForm.telegramChatId
                  : !notifForm.webhookUrl)
              }
            >
              <Send className="h-3.5 w-3.5" />
              {testingNotifModal ? 'Sending...' : 'Send Test Alert'}
            </Button>
            <div className="flex items-center gap-2">
              <Button type="button" variant="ghost" onClick={() => setNotifModalOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={savingNotif}>
                {savingNotif
                  ? 'Saving...'
                  : editingNotif
                    ? 'Save Channel'
                    : 'Add Channel'}
              </Button>
            </div>
          </div>
        </form>
      </Modal>
    </div>
  )
}
