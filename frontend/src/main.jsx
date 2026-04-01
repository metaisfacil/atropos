import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import './index.css'
import { GetLaunchArgs } from '../wailsjs/go/main/App'

const rootEl = document.getElementById('root')

if (!rootEl) {
  throw new Error('Root element #root was not found')
}

const applyRuntimeInteractionMode = async () => {
  const isDev = Boolean(import.meta?.env?.DEV)
  let isDebug = false
  try {
    const args = (await GetLaunchArgs()) || {}
    isDebug = Boolean(args.debug)
  } catch {
    isDebug = false
  }
  document.body.classList.toggle('app-runtime-lockdown', !isDev && !isDebug)
}

applyRuntimeInteractionMode()

ReactDOM.createRoot(rootEl).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
)