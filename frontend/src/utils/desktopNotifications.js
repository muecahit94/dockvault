const STORAGE_KEY = 'dockvault.desktopNotifications'

export function desktopNotificationsSupported() {
  return typeof window !== 'undefined' && 'Notification' in window
}

export function desktopNotificationsEnabled() {
  if (!desktopNotificationsSupported()) return false
  try {
    return window.localStorage.getItem(STORAGE_KEY) === 'on' && window.Notification.permission === 'granted'
  } catch {
    return false
  }
}

// Requests browser permission when enabling; resolves to the effective enabled state.
export async function setDesktopNotificationsEnabled(enabled) {
  if (!desktopNotificationsSupported()) return false
  if (enabled && window.Notification.permission !== 'granted') {
    const permission = await window.Notification.requestPermission()
    if (permission !== 'granted') {
      enabled = false
    }
  }
  try {
    window.localStorage.setItem(STORAGE_KEY, enabled ? 'on' : 'off')
  } catch {
    // Storage unavailable (private mode); the setting simply won't persist.
  }
  return enabled
}

export function showDesktopNotification(title, body, tag) {
  if (!desktopNotificationsEnabled()) return
  try {
    const n = new window.Notification(title, { body, tag, icon: '/favicon.svg' })
    n.onclick = () => {
      window.focus()
      n.close()
    }
  } catch {
    // Some browsers (e.g. Android Chrome) only allow notifications from service workers.
  }
}
