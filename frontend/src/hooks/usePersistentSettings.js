import { useState, useEffect } from 'react'
import { SetTouchupSettings, SetWarpSettings, SetDiscSettings } from '../../wailsjs/go/main/App'

// Default values for every persisted setting. Keep in sync with NewApp() in app.go.
const DEFAULTS = {
  touchupBackend:   'patchmatch',
  iopaintURL:       'http://127.0.0.1:8086/',
  warpFillMode:     'clamp',
  warpFillColor:    '#ffffff',
  discCenterCutout: true,
  discCutoutPercent: 11,
}

export function usePersistentSettings({ setPreview }) {
  const [touchupBackend, setTouchupBackendState] = useState(() =>
    localStorage.getItem('touchupBackend') || DEFAULTS.touchupBackend
  )
  const [iopaintURL, setIopaintURLState] = useState(() =>
    localStorage.getItem('iopaintURL') || DEFAULTS.iopaintURL
  )

  // Persist and push to backend whenever either setting changes.
  const setTouchupBackend = (v) => {
    setTouchupBackendState(v)
    localStorage.setItem('touchupBackend', v)
    SetTouchupSettings({ backend: v, iopaintUrl: iopaintURL }).catch(() => {})
  }
  const setIopaintURL = (v) => {
    setIopaintURLState(v)
    localStorage.setItem('iopaintURL', v)
    SetTouchupSettings({ backend: touchupBackend, iopaintUrl: v }).catch(() => {})
  }

  const [warpFillMode, setWarpFillModeState] = useState(() =>
    localStorage.getItem('warpFillMode') || DEFAULTS.warpFillMode
  )
  const [warpFillColor, setWarpFillColorState] = useState(() =>
    localStorage.getItem('warpFillColor') || DEFAULTS.warpFillColor
  )

  const setWarpFillMode = (v) => {
    setWarpFillModeState(v)
    localStorage.setItem('warpFillMode', v)
    SetWarpSettings({ fillMode: v, fillColor: warpFillColor }).catch(() => {})
  }
  const setWarpFillColor = (v) => {
    setWarpFillColorState(v)
    localStorage.setItem('warpFillColor', v)
    SetWarpSettings({ fillMode: warpFillMode, fillColor: v }).catch(() => {})
  }

  const [discCenterCutout, setDiscCenterCutoutState] = useState(() => {
    const stored = localStorage.getItem('discCenterCutout')
    return stored === null ? DEFAULTS.discCenterCutout : stored === 'true'
  })

  const [discCutoutPercent, setDiscCutoutPercentState] = useState(() =>
    parseInt(localStorage.getItem('discCutoutPercent') || String(DEFAULTS.discCutoutPercent), 10)
  )

  const setDiscCenterCutout = (v) => {
    setDiscCenterCutoutState(v)
    localStorage.setItem('discCenterCutout', String(v))
    SetDiscSettings({ centerCutout: v, cutoutPercent: discCutoutPercent }).then((result) => {
      if (result?.preview) setPreview(result.preview)
    }).catch(() => {})
  }

  const setDiscCutoutPercent = (v) => {
    setDiscCutoutPercentState(v)
    localStorage.setItem('discCutoutPercent', String(v))
  }

  const [closeAfterSave, setCloseAfterSaveState] = useState(() =>
    localStorage.getItem('closeAfterSave') === 'true'
  )
  const setCloseAfterSave = (v) => {
    setCloseAfterSaveState(v)
    localStorage.setItem('closeAfterSave', String(v))
  }

  const [postSaveEnabled, setPostSaveEnabledState] = useState(() =>
    localStorage.getItem('postSaveEnabled') === 'true'
  )
  const setPostSaveEnabled = (v) => {
    setPostSaveEnabledState(v)
    localStorage.setItem('postSaveEnabled', String(v))
  }

  const [postSaveCommand, setPostSaveCommandState] = useState(() =>
    localStorage.getItem('postSaveCommand') || ''
  )
  const setPostSaveCommand = (v) => {
    setPostSaveCommandState(v)
    localStorage.setItem('postSaveCommand', v)
  }

  // Push all persisted settings to backend on startup.
  useEffect(() => {
    SetTouchupSettings({
      backend:    localStorage.getItem('touchupBackend') || DEFAULTS.touchupBackend,
      iopaintUrl: localStorage.getItem('iopaintURL')     || DEFAULTS.iopaintURL,
    }).catch(() => {})
    SetWarpSettings({
      fillMode:  localStorage.getItem('warpFillMode')  || DEFAULTS.warpFillMode,
      fillColor: localStorage.getItem('warpFillColor') || DEFAULTS.warpFillColor,
    }).catch(() => {})
    const storedCutout  = localStorage.getItem('discCenterCutout')
    const storedPercent = parseInt(localStorage.getItem('discCutoutPercent') || String(DEFAULTS.discCutoutPercent), 10)
    SetDiscSettings({
      centerCutout:  storedCutout === null ? DEFAULTS.discCenterCutout : storedCutout === 'true',
      cutoutPercent: storedPercent,
    }).catch(() => {})
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  return {
    touchupBackend, setTouchupBackend,
    iopaintURL, setIopaintURL,
    warpFillMode, setWarpFillMode,
    warpFillColor, setWarpFillColor,
    discCenterCutout, setDiscCenterCutout,
    discCutoutPercent, setDiscCutoutPercent,
    closeAfterSave, setCloseAfterSave,
    postSaveEnabled, setPostSaveEnabled,
    postSaveCommand, setPostSaveCommand,
  }
}
