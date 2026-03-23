import { useEffect } from 'react'
import {
  Crop, Rotate, ShiftDisc, RotateDisc, SetFeatherSize, GetPixelColor, ConfirmClose,
} from '../../wailsjs/go/main/App'
import { Quit } from '../../wailsjs/runtime/runtime'

export function useKeyboardShortcuts({
  imageLoaded, mode, discActive, featherSize, discRotation,
  ctrlDragRef, shiftDragRef, mousePosRef,
  setPreview, setFeatherSize, setLoading, setRealImageDims,
  setDiscNoMaskPreview, setDiscCenter, setDiscRadius, setDiscBgColor, setDiscRotation,
  displayToImage, showStatus, showError, handleSaveImage, flushPendingSave, handleLoadImage, canSave,
  normalRect, handleNormalCrop, handleUndo,
  unsavedChanges, setUnsavedChanges, confirmClose,
}) {
  useEffect(() => {
    const handleKeyDown = async (e) => {
      if ((e.ctrlKey || e.metaKey) && e.code === 'KeyW') {
        e.preventDefault()
        if (unsavedChanges) {
          const saveFirst = window.confirm('You have unsaved changes. Save before quitting?')
          if (saveFirst) {
            const saved = await handleSaveImage()
            if (saved) {
              await confirmClose();
              Quit()
            }
            return
          }
          const exitWithoutSave = window.confirm('Quit without saving your changes?')
          if (exitWithoutSave) {
            await confirmClose();
            Quit()
          }
          return
        }
        await confirmClose();
        Quit()
        return
      }
      if ((e.ctrlKey || e.metaKey) && e.code === 'KeyO') {
        e.preventDefault()
        try {
          await handleLoadImage()
        } catch (err) {
          console.error('Open shortcut error:', err)
          showError(err)
        }
        return
      }
      if (!imageLoaded) return
      try {
        let result

        if (mode === 'disc' && discActive) {
          const shiftStep = e.shiftKey ? 20 : 5
          // Rotate the visual arrow direction into image space so that
          // nudging honours the current disc rotation.
          const applyShift = async (visualDx, visualDy) => {
            const rad = (discRotation || 0) * Math.PI / 180
            const cos = Math.cos(rad)
            const sin = Math.sin(rad)
            const dx = Math.round(cos * visualDx + sin * visualDy)
            const dy = Math.round(-sin * visualDx + cos * visualDy)
            const r = await ShiftDisc({ dx, dy })
            if (r?.preview) setPreview(r.preview)
            if (r?.unmaskedPreview) setDiscNoMaskPreview(r.unmaskedPreview)
            if (r?.discCenterX !== undefined && r?.discCenterY !== undefined) setDiscCenter({ x: r.discCenterX, y: r.discCenterY })
            if (r?.discRadius !== undefined) setDiscRadius(r.discRadius)
            if (r?.discRotation !== undefined) setDiscRotation(r.discRotation)
            if (r?.discBgR !== undefined) setDiscBgColor({ r: r.discBgR, g: r.discBgG, b: r.discBgB })
          }
          switch (e.key) {
            case 'ArrowUp':    e.preventDefault(); await applyShift(0, -shiftStep); return
            case 'ArrowDown':  e.preventDefault(); await applyShift(0,  shiftStep); return
            case 'ArrowLeft':  e.preventDefault(); await applyShift(-shiftStep, 0); return
            case 'ArrowRight': e.preventDefault(); await applyShift( shiftStep, 0); return
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
          case 'w': result = await Crop({ direction: 'top'    }); if (result?.preview) setPreview(result.preview); if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height }); setUnsavedChanges(true); await flushPendingSave(); break
          case 's': result = await Crop({ direction: 'bottom' }); if (result?.preview) setPreview(result.preview); if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height }); setUnsavedChanges(true); await flushPendingSave(); break
          case 'a': result = await Crop({ direction: 'left'   }); if (result?.preview) setPreview(result.preview); if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height }); setUnsavedChanges(true); await flushPendingSave(); break
          case 'd': result = await Crop({ direction: 'right'  }); if (result?.preview) setPreview(result.preview); if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height }); setUnsavedChanges(true); await flushPendingSave(); break
          case 'q':
            setLoading(true); showStatus('Rotating…')
            result = mode === 'disc' && discActive
              ? await RotateDisc({ angle: -15 })
              : await Rotate({ flipCode: 2 })
            if (result?.preview) setPreview(result.preview)
            if (result?.unmaskedPreview) setDiscNoMaskPreview(result.unmaskedPreview)
            if (result?.discCenterX !== undefined && result?.discCenterY !== undefined) setDiscCenter({ x: result.discCenterX, y: result.discCenterY })
            if (result?.discRadius !== undefined) setDiscRadius(result.discRadius)
            if (result?.discRotation !== undefined) setDiscRotation(result.discRotation)
            if (result?.discBgR !== undefined) setDiscBgColor({ r: result.discBgR, g: result.discBgG, b: result.discBgB })
            if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height })
            setUnsavedChanges(true)
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
            if (result?.discRotation !== undefined) setDiscRotation(result.discRotation)
            if (result?.discBgR !== undefined) setDiscBgColor({ r: result.discBgR, g: result.discBgG, b: result.discBgB })
            if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height })
            setUnsavedChanges(true)
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
  }, [imageLoaded, mode, discActive, featherSize, discRotation, displayToImage, normalRect, handleNormalCrop, handleUndo, canSave, handleLoadImage]) // eslint-disable-line react-hooks/exhaustive-deps
}
