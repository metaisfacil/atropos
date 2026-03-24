// Frontend debug log utility shared across the app.
// This is intentionally lightweight and app-level; it can store logs
// for later forwarding via App.LogFrontend when --debug is enabled.

function getStore() {
  if (typeof window === 'undefined') return null
  if (!window.__atropos_debugLogs) window.__atropos_debugLogs = []
  return window.__atropos_debugLogs
}

function abbreviateBase64String(str) {
  if (typeof str !== 'string') return str
  const marker = 'base64,'
  const idx = str.indexOf(marker)
  if (idx === -1 || str.length <= 256) return str

  const prefix = str.slice(0, idx + marker.length)
  const content = str.slice(idx + marker.length)
  const head = content.slice(0, 80)
  const tail = content.slice(-80)
  return `${prefix}${head}...[${content.length} bytes]...${tail}`
}

function sanitizeArg(arg) {
  if (typeof arg === 'string') return abbreviateBase64String(arg)
  if (Array.isArray(arg)) return arg.map(sanitizeArg)
  if (arg && typeof arg === 'object') {
    const out = {}
    for (const [k, v] of Object.entries(arg)) {
      out[k] = sanitizeArg(v)
    }
    return out
  }
  return arg
}

export const debugOptions = {
  verbose: false, // set true to emit high-volume internal logs like computeDiscShift
  forwardToBackend: false, // set true to send console logs to backend LogFrontend
}

export function setDebugVerbose(on = true) {
  debugOptions.verbose = Boolean(on)
  debugOptions.forwardToBackend = Boolean(on)
}

export { sanitizeArg }

export function addDebugLog(type, data) {
  if (!debugOptions.verbose) return
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

  const originalConsole = {
    debug: window.console.debug.bind(window.console),
    info: window.console.info.bind(window.console),
    warn: window.console.warn.bind(window.console),
    error: window.console.error.bind(window.console),
  }

  const abbreviateBase64 = (str) => {
    if (typeof str !== 'string') return str
    const marker = 'base64,'
    const idx = str.indexOf(marker)
    if (idx === -1 || str.length < 256) return str
    const prefix = str.slice(0, idx + marker.length)
    const data = str.slice(idx + marker.length)
    const visibleFirst = data.slice(0, 80)
    const visibleLast = data.slice(-80)
    return `${prefix}${visibleFirst}...[${data.length} bytes]...${visibleLast}`
  }

  const forward = async (level, args) => {
    if (!debugOptions.forwardToBackend) return
    try {
      const safeArgs = args.map(sanitizeArg)
      addDebugLog('console.' + level, { args: safeArgs, ts: new Date().toISOString() })
      if (window.go?.main?.App?.LogFrontend) {
        const payload = `[FE][console.${level}] ${safeArgs.map(a => (typeof a === 'string' ? a : JSON.stringify(a))).join(' ')} `
        await window.go.main.App.LogFrontend(payload)
      }
    } catch (err) {
      // Keep working even when frontend->backend bridge is not ready
      originalConsole.warn('Forwarding console logs to backend failed', err)
    }
  }

  window.console.debug = (...args) => {
    originalConsole.debug(...args)
    forward('debug', args)
  }
  window.console.info = (...args) => {
    originalConsole.info(...args)
    forward('info', args)
  }
  window.console.warn = (...args) => {
    originalConsole.warn(...args)
    forward('warn', args)
  }
  window.console.error = (...args) => {
    originalConsole.error(...args)
    forward('error', args)
  }
}
