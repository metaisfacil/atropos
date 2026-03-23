import { useEffect } from 'react'
import {
  Crop, Rotate, ShiftDisc, RotateDisc, SetFeatherSize, GetPixelColor,
} from '../../wailsjs/go/main/App'
import { Quit } from '../../wailsjs/runtime/runtime'

export function useKeyboardShortcuts({
  imageLoaded, mode, discActive, featherSize,
  ctrlDragRef, shiftDragRef, mousePosRef,
  setPreview, setFeatherSize, setLoading, setRealImageDims,
  setDiscNoMaskPreview, setDiscCenter, setDiscRadius, setDiscBgColor,
  displayToImage, showStatus, showError, handleSaveImage, flushPendingSave, canSave,
  normalRect, handleNormalCrop, handleUndo,
}) {
  useEffect(() => {
    const handleKeyDown = async (e) => {
      if ((e.ctrlKey || e.metaKey) && e.code === 'KeyW') {
        e.preventDefault()
        Quit()
        return
      }
      if (!imageLoaded) return
      try {
        let result

        if (mode === 'disc' && discActive) {
          const shiftStep = e.shiftKey ? 20 : 5
          switch (e.key) {
            case 'ArrowUp':    e.preventDefault(); result = await ShiftDisc({ dx: 0, dy: -shiftStep }); if (result?.preview) setPreview(result.preview); return
            case 'ArrowDown':  e.preventDefault(); result = await ShiftDisc({ dx: 0, dy:  shiftStep }); if (result?.preview) setPreview(result.preview); return
            case 'ArrowLeft':  e.preventDefault(); result = await ShiftDisc({ dx: -shiftStep, dy: 0 }); if (result?.preview) setPreview(result.preview); return
            case 'ArrowRight': e.preventDefault(); result = await ShiftDisc({ dx:  shiftStep, dy: 0 }); if (result?.preview) setPreview(result.preview); return
            case '+': case '=': {
              const newF = Math.min(100, featherSize + 1); setFeatherSize(newF)
              result = await SetFeatherSize({ size: newF }); if (result?.preview) setPreview(result.preview); return
            }
            case '-': {
              const newF = Math.max(0, featherSize - 1); setFeatherSize(newF)
              result = await SetFeatherSize({ size: newF }); if (result?.preview) setPreview(result.preview); return
            }
            case 'y': case 'Y': {
              const mp = mousePosRef.current
              const imgPt = displayToImage(mp.x, mp.y)
              result = await GetPixelColor({ x: imgPt.x, y: imgPt.y })
              if (result?.preview) setPreview(result.preview)
              return
            }
          }
        }

        const key = e.key.toLowerCase()
        if ((e.ctrlKey || e.metaKey) && e.code === 'KeyZ') {
          if (e.repeat) return
          const active = document.activeElement
          if (active && (['INPUT', 'TEXTAREA', 'SELECT'].includes(active.tagName) || active.isContentEditable)) return
          if (ctrlDragRef.current !== null || shiftDragRef.current !== null) return
          e.preventDefault()
          await handleUndo()
          return
        }

        if ((e.ctrlKey || e.metaKey) && e.code === 'KeyS') {
          if (e.repeat) return
          e.preventDefault()
          if (!canSave) return
          try {
            await handleSaveImage()
          } catch (err) {
            console.error('Save shortcut error:', err)
            showError(err)
          }
          return
        }

        if (e.key === 'Enter' && mode === 'normal' && normalRect) {
          e.preventDefault()
          await handleNormalCrop()
          return
        }

        if (['w', 's', 'a', 'd', 'q', 'e'].includes(key)) {
          if (!canSave) { showStatus('Apply a crop first before adjusting'); return }
        }

        switch (key) {
          case 'w': result = await Crop({ direction: 'top'    }); if (result?.preview) setPreview(result.preview); if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height }); await flushPendingSave(); break
          case 's': result = await Crop({ direction: 'bottom' }); if (result?.preview) setPreview(result.preview); if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height }); await flushPendingSave(); break
          case 'a': result = await Crop({ direction: 'left'   }); if (result?.preview) setPreview(result.preview); if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height }); await flushPendingSave(); break
          case 'd': result = await Crop({ direction: 'right'  }); if (result?.preview) setPreview(result.preview); if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height }); await flushPendingSave(); break
          case 'q':
            setLoading(true); showStatus('Rotating…')
            result = mode === 'disc' && discActive
              ? await RotateDisc({ angle: -15 })
              : await Rotate({ flipCode: 2 })
            if (result?.preview) setPreview(result.preview)
            if (result?.unmaskedPreview) setDiscNoMaskPreview(result.unmaskedPreview)
            if (result?.discCenterX !== undefined && result?.discCenterY !== undefined) setDiscCenter({ x: result.discCenterX, y: result.discCenterY })
            if (result?.discRadius !== undefined) setDiscRadius(result.discRadius)
            if (result?.discBgR !== undefined) setDiscBgColor({ r: result.discBgR, g: result.discBgG, b: result.discBgB })
            if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height })
            showStatus(''); setLoading(false); await flushPendingSave(); break
          case 'e':
            setLoading(true); showStatus('Rotating…')
            result = mode === 'disc' && discActive
              ? await RotateDisc({ angle: 15 })
              : await Rotate({ flipCode: 1 })
            if (result?.preview) setPreview(result.preview)
            if (result?.unmaskedPreview) setDiscNoMaskPreview(result.unmaskedPreview)
            if (result?.discCenterX !== undefined && result?.discCenterY !== undefined) setDiscCenter({ x: result.discCenterX, y: result.discCenterY })
            if (result?.discRadius !== undefined) setDiscRadius(result.discRadius)
            if (result?.discBgR !== undefined) setDiscBgColor({ r: result.discBgR, g: result.discBgG, b: result.discBgB })
            if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height })
            showStatus(''); setLoading(false); await flushPendingSave(); break
          default:
            break
        }
      } catch (err) {
        console.error('Shortcut error:', err)
        showError(err)
        setLoading(false)
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [imageLoaded, mode, discActive, featherSize, displayToImage, normalRect, handleNormalCrop, handleUndo, canSave]) // eslint-disable-line react-hooks/exhaustive-deps
}
