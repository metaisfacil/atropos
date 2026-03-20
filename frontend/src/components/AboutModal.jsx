import { useEffect, useRef, useState } from 'react'
import { GetVersion } from '../../wailsjs/go/main/App'
import { BrowserOpenURL } from '../../wailsjs/runtime/runtime'

const FADE_MS = 150
const GITHUB_URL = 'https://github.com/metaisfacil/atropos'

export default function AboutModal({ open, onClose }) {
  const [mounted, setMounted] = useState(false)
  const [shown, setShown]     = useState(false)
  const [version, setVersion] = useState('')
  const fadeOutTimer = useRef(null)

  useEffect(() => {
    GetVersion().then(setVersion).catch(() => {})
  }, [])

  useEffect(() => {
    if (open) {
      clearTimeout(fadeOutTimer.current)
      setMounted(true)
      requestAnimationFrame(() => requestAnimationFrame(() => setShown(true)))
    } else {
      setShown(false)
      fadeOutTimer.current = setTimeout(() => setMounted(false), FADE_MS)
    }
    return () => clearTimeout(fadeOutTimer.current)
  }, [open])

  useEffect(() => {
    if (!open) return
    const handler = (e) => { if (e.key === 'Escape' || e.key === 'Enter') onClose() }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [open, onClose])

  if (!mounted) return null

  return (
    <div className={`options-backdrop ${shown ? 'visible' : ''}`} onClick={onClose}>
      <div
        className="options-dialog about-dialog"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-label="About"
      >
        <div className="options-header">
          <span className="options-title">About</span>
          <button className="options-close" onClick={onClose} aria-label="Close">✕</button>
        </div>

        <div className="options-body about-body">
          <img src="/appicon.png" alt="Atropos icon" className="about-icon" />
          <div className="about-name">Atropos</div>
          <div className="about-version">Version {version}</div>
          <div className="about-description">Desktop image processing tool<br />for musical materials</div>
          <div className="about-author">by metaisfacil</div>
          <button
            className="about-link"
            onClick={() => BrowserOpenURL(GITHUB_URL)}
          >
            {GITHUB_URL}
          </button>
        </div>

        <div className="options-footer">
          <button className="options-ok-btn" onClick={onClose}>OK</button>
        </div>
      </div>
    </div>
  )
}
