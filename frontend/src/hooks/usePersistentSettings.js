import { useState, useEffect, useRef } from 'react'
import { GetAllSettings, SaveAllSettings, SetDiscSettings } from '../../wailsjs/go/main/App'

// Compiled-in defaults — must stay in sync with NewApp() in app.go and
// AllSettings defaults in GetAllSettings().
// Note: the persisted JSON key for the IOPaint URL is `iopaintUrl` (see
// Go `AllSettings.IOPaintURL` JSON tag). The frontend API surface exposes
// this setting as `iopaintURL` when returning values from this hook; the
// internal defaults and persistence keys use `iopaintUrl` to match the
// generated Wails models and Go JSON fields.
const DEFAULTS = {
  touchupBackend:            'patchmatch',
  iopaintUrl:                'http://127.0.0.1:8086/',
  warpFillMode:              'clamp',
  warpFillColor:             '#ffffff',
  discCenterCutout:          true,
  discCutoutPercent:         11,
  cornerMaxCorners:          500,
  cornerMinDistance:         100,
  cornerSettingsVersion:     1,
  autoCornerParams:          false,
  closeAfterSave:            false,
  postSaveEnabled:           false,
  postSaveCommand:           '',
  touchupRemainsActive:      true,
  straightEdgeRemainsActive: true,
  autoDetectOnModeSwitch:    true,
}

// migrateFromLocalStorage overlays any values that were saved by the old
// localStorage-based settings system onto the provided defaults object.
// Only keys that are actually present in localStorage (i.e. were explicitly
// set by the user) override the defaults; absent keys leave the default in
// place.  Called once, the first time the new version runs.
function migrateFromLocalStorage(base) {
  const result = { ...base }
  const str  = (key) => localStorage.getItem(key)
  const bool = (key) => { const v = localStorage.getItem(key); return v === null ? null : v === 'true' }
  const num  = (key) => { const v = localStorage.getItem(key); return v === null ? null : parseInt(v, 10) }

  if (str('touchupBackend')           !== null) result.touchupBackend           = str('touchupBackend')
  if (str('iopaintURL')               !== null) result.iopaintUrl               = str('iopaintURL')
  if (str('warpFillMode')             !== null) result.warpFillMode             = str('warpFillMode')
  if (str('warpFillColor')            !== null) result.warpFillColor            = str('warpFillColor')
  if (bool('discCenterCutout')        !== null) result.discCenterCutout         = bool('discCenterCutout')
  if (num('discCutoutPercent')        !== null) result.discCutoutPercent        = num('discCutoutPercent')
  if (bool('autoCornerParams')        !== null) result.autoCornerParams         = bool('autoCornerParams')
  if (bool('closeAfterSave')          !== null) result.closeAfterSave           = bool('closeAfterSave')
  if (bool('postSaveEnabled')         !== null) result.postSaveEnabled          = bool('postSaveEnabled')
  if (str('postSaveCommand')          !== null) result.postSaveCommand          = str('postSaveCommand')
  if (bool('touchupRemainsActive')    !== null) result.touchupRemainsActive     = bool('touchupRemainsActive')
  if (bool('straightEdgeRemainsActive') !== null) result.straightEdgeRemainsActive = bool('straightEdgeRemainsActive')
  if (bool('autoDetectOnModeSwitch')  !== null) result.autoDetectOnModeSwitch   = bool('autoDetectOnModeSwitch')

  return result
}

export function usePersistentSettings({ setPreview }) {
  // Initialise from defaults; overwritten by GetAllSettings() on mount.
  const [settings, setSettings] = useState(DEFAULTS)
  const settingsRef = useRef(DEFAULTS)

  // Load from the shared settings file on mount.  When the file does not
  // exist yet (s.initialized === false) we perform a one-time migration from
  // any localStorage values written by an older version of the app, then
  // immediately persist to the new file so the migration never runs again.
  useEffect(() => {
    GetAllSettings().then((s) => {
      let merged = { ...DEFAULTS, ...s }
      let needsSave = false
      if (!s.initialized) {
        merged = migrateFromLocalStorage(merged)
        needsSave = true
      } else if ((s.cornerSettingsVersion ?? 0) < 1) {
        // Older settings files contain autoCornerParams=true because that was
        // the former default, not necessarily because the user selected it.
        // Migrate once to stable manual parameters; subsequent opt-in choices
        // are preserved normally by cornerSettingsVersion.
        merged = {
          ...merged,
          autoCornerParams: false,
          cornerMaxCorners: DEFAULTS.cornerMaxCorners,
          cornerMinDistance: DEFAULTS.cornerMinDistance,
          cornerSettingsVersion: 1,
        }
        needsSave = true
      }
      if (needsSave) SaveAllSettings(merged).catch(() => {})
      settingsRef.current = merged
      setSettings(merged)
    }).catch(() => {})
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // Helper: update one key, persist the whole object to disk.
  function update(key, value) {
    const next = { ...settingsRef.current, [key]: value }
    settingsRef.current = next
    setSettings(next)
    SaveAllSettings(next).catch(() => {})
  }

  // Individual setters — same interface the rest of the app already uses.
  const setTouchupBackend = (v) => update('touchupBackend', v)
  const setIopaintURL     = (v) => update('iopaintUrl', v)
  const setWarpFillMode   = (v) => update('warpFillMode', v)
  const setWarpFillColor  = (v) => update('warpFillColor', v)
  const setCornerMaxCorners           = (v) => update('cornerMaxCorners', v)
  const setCornerMinDistance          = (v) => update('cornerMinDistance', v)
  const setAutoCornerParams          = (v) => update('autoCornerParams', v)
  const setCloseAfterSave            = (v) => update('closeAfterSave', v)
  const setPostSaveEnabled           = (v) => update('postSaveEnabled', v)
  const setPostSaveCommand           = (v) => update('postSaveCommand', v)
  const setTouchupRemainsActive      = (v) => update('touchupRemainsActive', v)
  const setStraightEdgeRemainsActive = (v) => update('straightEdgeRemainsActive', v)
  const setAutoDetectOnModeSwitch    = (v) => update('autoDetectOnModeSwitch', v)

  // Disc settings also trigger a live re-render via SetDiscSettings.
  const setDiscCenterCutout = (v) => {
    update('discCenterCutout', v)
    SetDiscSettings({ centerCutout: v, cutoutPercent: settingsRef.current.discCutoutPercent })
      .then((result) => { if (result?.preview) setPreview(result.preview) })
      .catch(() => {})
  }
  const setDiscCutoutPercent = (v) => update('discCutoutPercent', v)

  // History already restored the backend pixels and parameters. Synchronize
  // controls without SetDiscSettings, which would render and branch history.
  const syncHistoryDiscSettings = (disc) => {
    const next = { ...settingsRef.current, discCenterCutout: disc.centerCutout, discCutoutPercent: disc.cutoutPercent }
    settingsRef.current = next
    setSettings(next)
    SaveAllSettings(next).catch(() => {})
  }

  return {
    syncHistoryDiscSettings,
    touchupBackend:            settings.touchupBackend,
    iopaintURL:                settings.iopaintUrl,
    warpFillMode:              settings.warpFillMode,
    warpFillColor:             settings.warpFillColor,
    discCenterCutout:          settings.discCenterCutout,
    discCutoutPercent:         settings.discCutoutPercent,
    cornerMaxCorners:          settings.cornerMaxCorners,
    cornerMinDistance:         settings.cornerMinDistance,
    autoCornerParams:          settings.autoCornerParams,
    closeAfterSave:            settings.closeAfterSave,
    postSaveEnabled:           settings.postSaveEnabled,
    postSaveCommand:           settings.postSaveCommand,
    touchupRemainsActive:      settings.touchupRemainsActive,
    straightEdgeRemainsActive: settings.straightEdgeRemainsActive,
    autoDetectOnModeSwitch:    settings.autoDetectOnModeSwitch,
    setTouchupBackend,
    setIopaintURL,
    setWarpFillMode,
    setWarpFillColor,
    setDiscCenterCutout,
    setDiscCutoutPercent,
    setCornerMaxCorners,
    setCornerMinDistance,
    setAutoCornerParams,
    setCloseAfterSave,
    setPostSaveEnabled,
    setPostSaveCommand,
    setTouchupRemainsActive,
    setStraightEdgeRemainsActive,
    setAutoDetectOnModeSwitch,
  }
}
