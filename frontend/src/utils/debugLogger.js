// Frontend debug log utility shared across the app.
// This is intentionally lightweight and app-level; it can store logs
// for later forwarding via App.LogFrontend when --debug is enabled.

function getStore() {
  if (typeof window === 'undefined') return null
  if (!window.__atropos_debugLogs) window.__atropos_debugLogs = []
  return window.__atropos_debugLogs
}

export function addDebugLog(type, data) {
  const store = getStore()
  if (!store) return
  store.push({ type, data, ts: new Date().toISOString() })
}

export function getDebugLogs() {
  return getStore() || []
}

export function clearDebugLogs() {
  if (typeof window === 'undefined') return
  window.__atropos_debugLogs = []
}

export async function forwardDebugLogsToBackend() {
  if (typeof window === 'undefined') return
  const store = getStore() || []
  const payload = store.map((item, index) => `Entry ${index + 1}: ${JSON.stringify(item)}`).join('\n\n')

  if (!payload) {
    console.info('No debug logs to forward')
    return
  }

  if (window.go?.main?.App?.LogFrontend) {
    try {
      await window.go.main.App.LogFrontend(payload)
      console.info(`Forwarded ${store.length} frontend debug log entries to backend LogFrontend.`)
      return
    } catch (err) {
      console.warn('Failed to forward debug logs to backend LogFrontend', err)
    }
  } else {
    console.warn('LogFrontend is not available. Ensure the app is running with Wails and --debug flag.')
  }
}

export function initFrontendDebugLogAPI() {
  if (typeof window === 'undefined') return
  window.copyShiftDiscLogs = forwardDebugLogsToBackend
  window.clearShiftDiscLogs = clearDebugLogs
  window.copyFrontendDebugLogs = forwardDebugLogsToBackend
  window.clearFrontendDebugLogs = clearDebugLogs
}
