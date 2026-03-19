import { useState, useRef } from 'react'

export function useStatusMessage() {
  const [imageInfo, setImageInfo]               = useState('')
  const [imageInfoVisible, setImageInfoVisible] = useState(true)
  const statusFadeTimer  = useRef(null)
  const statusClearTimer = useRef(null)

  const showStatus = (msg) => {
    clearTimeout(statusFadeTimer.current)
    clearTimeout(statusClearTimer.current)
    setImageInfo(msg)
    setImageInfoVisible(true)
    if (msg) {
      statusFadeTimer.current  = setTimeout(() => setImageInfoVisible(false), 4000)
      statusClearTimer.current = setTimeout(() => setImageInfo(''), 5000)
    }
  }

  return { imageInfo, imageInfoVisible, showStatus }
}
