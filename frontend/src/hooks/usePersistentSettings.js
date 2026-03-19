import { useState, useEffect } from 'react'
import { SetTouchupSettings, SetWarpSettings, SetDiscSettings } from '../../wailsjs/go/main/App'

export function usePersistentSettings({ setPreview }) {
  const [touchupBackend, setTouchupBackendState] = useState(() =>
    localStorage.getItem('touchupBackend') || 'patchmatch'
  )
  const [iopaintURL, setIopaintURLState] = useState(() =>
    localStorage.getItem('iopaintURL') || 'http://127.0.0.1:8086/'
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
    localStorage.getItem('warpFillMode') || 'clamp'
  )
  const [warpFillColor, setWarpFillColorState] = useState(() =>
    localStorage.getItem('warpFillColor') || '#ffffff'
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
    return stored === null ? true : stored === 'true'
  })

  const [discCutoutPercent, setDiscCutoutPercentState] = useState(() =>
    parseInt(localStorage.getItem('discCutoutPercent') || '11', 10)
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

  // Push all persisted settings to backend on startup.
  useEffect(() => {
    SetTouchupSettings({
      backend: localStorage.getItem('touchupBackend') || 'patchmatch',
      iopaintUrl: localStorage.getItem('iopaintURL') || 'http://127.0.0.1:8086/',
    }).catch(() => {})
    SetWarpSettings({
      fillMode:  localStorage.getItem('warpFillMode')  || 'clamp',
      fillColor: localStorage.getItem('warpFillColor') || '#ffffff',
    }).catch(() => {})
    const storedCutout = localStorage.getItem('discCenterCutout')
    const storedPercent = parseInt(localStorage.getItem('discCutoutPercent') || '11', 10)
    SetDiscSettings({
      centerCutout: storedCutout === null ? true : storedCutout === 'true',
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
  }
}
