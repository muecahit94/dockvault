const API_BASE = '/api'

let unauthorizedHandler = null

export function setUnauthorizedHandler(fn) {
  unauthorizedHandler = fn
}

async function request(path, options = {}) {
  const res = await fetch(`${API_BASE}${path}`, {
    credentials: 'same-origin',
    headers: {
      'Content-Type': 'application/json',
      ...(options.headers || {}),
    },
    ...options,
  })

  if (res.status === 401 && unauthorizedHandler && !path.startsWith('/auth/')) {
    unauthorizedHandler()
  }

  const text = await res.text()
  let data = null
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    data = null
  }

  if (!res.ok) {
    const message = data?.error || data?.message || `Request failed (${res.status})`
    throw new Error(message)
  }

  return data
}

export const api = {
  getAuthStatus: () => request('/auth/status'),
  setupAuth: (payload) =>
    request('/auth/setup', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),
  login: (payload) =>
    request('/auth/login', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),
  logout: () =>
    request('/auth/logout', {
      method: 'POST',
    }),
  updateAuthSettings: (payload) =>
    request('/auth/settings', {
      method: 'PUT',
      body: JSON.stringify(payload),
    }),

  getHealth: () => request('/health'),
  getDashboard: () => request('/dashboard'),
  getContainers: (all = true) => request(`/containers?all=${all}`),
  getStacks: (all = true) => request(`/stacks?all=${all}`),
  getVolumes: () => request('/volumes'),
  deleteVolume: (name) =>
    request(`/volumes/${encodeURIComponent(name)}`, {
      method: 'DELETE',
    }),

  getBackups: (params = {}) => {
    const search = new URLSearchParams()
    if (params.status) search.set('status', params.status)
    if (params.target) search.set('target', params.target)
    if (params.limit) search.set('limit', String(params.limit))
    const qs = search.toString()
    return request(`/backups${qs ? `?${qs}` : ''}`)
  },
  runBackup: (payload) =>
    request('/backups/run', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),
  togglePinBackup: (id) =>
    request(`/backups/${id}/pin`, {
      method: 'POST',
    }),
  deleteBackup: (id, deleteFile = true) =>
    request(`/backups/${id}?deleteFile=${deleteFile}`, {
      method: 'DELETE',
    }),
  createBackupSasLink: (id) =>
    request(`/backups/${id}/sas`, {
      method: 'POST',
    }),
  inspectBackup: (id) => request(`/backups/${id}/inspect`),
  verifyBackup: (id) =>
    request(`/backups/${id}/verify`, {
      method: 'POST',
    }),
  restoreBackup: (id, options = true) => {
    const payload =
      typeof options === 'boolean'
        ? { pauseContainer: options }
        : {
            pauseContainer: options?.pauseContainer ?? true,
            targetVolumeName: options?.targetVolumeName || '',
            preRestoreBackup: Boolean(options?.preRestoreBackup),
          }
    return request(`/backups/${id}/restore`, {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  getBackupProgress: (id) => request(`/backups/${id}/progress`),
  backupEventsUrl: () => `${API_BASE}/backups/events`,
  downloadBackupUrl: (id, raw = false) =>
    `${API_BASE}/backups/${id}/download${raw ? '?raw=true' : ''}`,
  extractBackupFileUrl: (id, path) =>
    `${API_BASE}/backups/${id}/file?path=${encodeURIComponent(path)}`,

  getSchedules: () => request('/schedules'),
  syncGitOpsSchedules: () =>
    request('/schedules/sync-labels', {
      method: 'POST',
    }),
  createSchedule: (payload) =>
    request('/schedules', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),
  updateSchedule: (id, payload) =>
    request(`/schedules/${id}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    }),
  toggleSchedule: (id) =>
    request(`/schedules/${id}/toggle`, {
      method: 'POST',
    }),
  triggerSchedule: (id) =>
    request(`/schedules/${id}/run`, {
      method: 'POST',
    }),
  deleteSchedule: (id) =>
    request(`/schedules/${id}`, {
      method: 'DELETE',
    }),
  previewCron: (cronExpr) =>
    request('/schedules/preview', {
      method: 'POST',
      body: JSON.stringify({ cronExpr }),
    }),

  getStorageProviders: () => request('/storage'),
  createStorageProvider: (payload) =>
    request('/storage', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),
  updateStorageProvider: (id, payload) =>
    request(`/storage/${id}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    }),
  deleteStorageProvider: (id) =>
    request(`/storage/${id}`, {
      method: 'DELETE',
    }),
  testStorageProvider: (id) =>
    request(`/storage/${id}/test`, {
      method: 'POST',
    }),
  testUnsavedStorageProvider: (payload) =>
    request('/storage/test', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),
  scanStorageProvider: (id) =>
    request(`/storage/${id}/scan`, {
      method: 'POST',
    }),
  importStorageBackups: (id, storagePaths) =>
    request(`/storage/${id}/import`, {
      method: 'POST',
      body: JSON.stringify({ storagePaths }),
    }),

  getNotifications: () => request('/notifications'),
  createNotification: (payload) =>
    request('/notifications', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),
  updateNotification: (id, payload) =>
    request(`/notifications/${id}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    }),
  toggleNotification: (id) =>
    request(`/notifications/${id}/toggle`, {
      method: 'POST',
    }),
  testNotification: (id, payload) =>
    request(`/notifications/${id}/test`, {
      method: 'POST',
      body: JSON.stringify(payload || {}),
    }),
  deleteNotification: (id) =>
    request(`/notifications/${id}`, {
      method: 'DELETE',
    }),
  updateNotifications: (payload) =>
    request('/notifications', {
      method: 'PUT',
      body: JSON.stringify(payload),
    }),
  testNotifications: (payload) =>
    request('/notifications/test', {
      method: 'POST',
      body: JSON.stringify(payload || {}),
    }),

  getUIPreferences: () => request('/settings/preferences'),
  updateUIPreferences: (payload) =>
    request('/settings/preferences', {
      method: 'PUT',
      body: JSON.stringify(payload),
    }),
  resetUIPreferences: () =>
    request('/settings/preferences/reset', {
      method: 'POST',
    }),
}

let globalTimeFormat = '24h'

export function setTimeFormatPreference(fmt) {
  globalTimeFormat = fmt === '12h' ? '12h' : '24h'
}

export function formatBytes(bytes) {
  if (!bytes || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const idx = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  const val = bytes / Math.pow(1024, idx)
  return `${val >= 10 || idx === 0 ? val.toFixed(1) : val.toFixed(2)} ${units[idx]}`
}

export function formatBytesPerSec(bps) {
  if (!bps || bps <= 0) return '—'
  return `${formatBytes(bps)}/s`
}

export function formatDuration(ms) {
  if (!ms || ms < 0) return '—'
  if (ms < 1000) return `${ms} ms`
  const secs = ms / 1000
  if (secs < 60) return `${secs.toFixed(1)}s`
  const mins = Math.floor(secs / 60)
  const rem = Math.round(secs % 60)
  return `${mins}m ${rem}s`
}

export function formatDateTime(iso) {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '—'
  return d.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: globalTimeFormat === '12h',
  })
}

const NOTIFICATION_PROVIDER_LABELS = {
  telegram: 'Telegram Bot',
  discord: 'Discord Webhook',
  slack: 'Slack Webhook',
  ntfy: 'ntfy Topic',
  generic: 'JSON Webhook',
}

export function summarizeNotificationConfig(cfg) {
  const provider = (cfg?.provider || 'discord').toLowerCase()
  const providerLabel = NOTIFICATION_PROVIDER_LABELS[provider] || provider
  const isTelegram = provider === 'telegram'
  const configured = isTelegram
    ? Boolean((cfg?.hasTelegramBotToken || cfg?.telegramBotToken) && (cfg?.telegramChatId || '').trim())
    : Boolean((cfg?.webhookUrl || '').trim())
  const enabled = Boolean(cfg?.enabled)
  const active = configured && enabled

  let targetSummary = ''
  if (configured) {
    if (isTelegram) {
      targetSummary = `Chat ${cfg.telegramChatId}`
      if (cfg.telegramThreadId) {
        targetSummary += ` · Topic #${cfg.telegramThreadId}`
      }
    } else {
      try {
        const parsed = new URL(cfg.webhookUrl)
        targetSummary = parsed.hostname + (parsed.pathname && parsed.pathname !== '/' ? '/…' : '')
      } catch {
        targetSummary = 'Webhook URL configured'
      }
    }
  }

  const triggerParts = []
  if (cfg?.onFailure) triggerParts.push('Failure')
  if (cfg?.onSuccess) triggerParts.push('Success')
  const triggers = triggerParts.length > 0 ? triggerParts.join(' & ') : 'No events enabled'

  return {
    configured,
    enabled,
    active,
    provider,
    providerLabel,
    displayName: (cfg?.name || '').trim() || providerLabel,
    targetSummary,
    triggers,
    hasToken: Boolean(cfg?.hasTelegramBotToken),
    updatedAt: cfg?.updatedAt || null,
  }
}

export function summarizeNotificationsList(list) {
  const rawList = Array.isArray(list) ? list : list ? [list] : []
  const channels = rawList.map((item) => {
    const summary = summarizeNotificationConfig(item)
    return {
      ...item,
      summary,
      displayName: summary.displayName,
    }
  })
  const activeChannels = channels.filter((c) => c.summary.active)
  return {
    channels,
    activeChannels,
    configuredCount: channels.length,
    activeCount: activeChannels.length,
    pausedCount: channels.length - activeChannels.length,
    firstActive: activeChannels[0] || null,
  }
}

export function formatRelativeTime(iso) {
  if (!iso) return '—'
  const target = new Date(iso).getTime()
  if (Number.isNaN(target)) return '—'
  const diffSec = Math.round((target - Date.now()) / 1000)
  const abs = Math.abs(diffSec)
  if (abs < 60) return diffSec >= 0 ? `in ${abs}s` : `${abs}s ago`
  const mins = Math.round(abs / 60)
  if (mins < 60) return diffSec >= 0 ? `in ${mins}m` : `${mins}m ago`
  const hrs = Math.round(mins / 60)
  if (hrs < 48) return diffSec >= 0 ? `in ${hrs}h` : `${hrs}h ago`
  const days = Math.round(hrs / 24)
  return diffSec >= 0 ? `in ${days}d` : `${days}d ago`
}
