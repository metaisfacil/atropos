import { useEffect } from 'react'
import {
  Crop, Rotate, ShiftDisc, RotateDisc, SetFeatherSize, GetPixelColor, ConfirmClose, CopySelectionToClipboard, UndoLastCorner,
} from '../../wailsjs/go/main/App'
import { Quit } from '../../wailsjs/runtime/runtime'
import { adjustmentSelectionPayload } from '../utils/adjustmentSelection'

export const KEYBOARD_CROP_AMOUNT = 3

export function optimisticCropDimensions(dims, direction, amount = KEYBOARD_CROP_AMOUNT) {
  if (!dims || !(dims.w > 0) || !(dims.h > 0)) return null
  const crop = Math.max(1, amount)
  return {
    w: ['left', 'right'].includes(direction) ? Math.max(1, dims.w - crop) : dims.w,
    h: ['top', 'bottom'].includes(direction) ? Math.max(1, dims.h - crop) : dims.h,
  }
}

export function useKeyboardShortcuts({
  imageLoaded, mode, discActive, featherSize, discRotation,
  ctrlDragRef, shiftDragRef, mousePosRef,
  setPreview, setFeatherSize, setLoading, setRealImageDims,
  preview, realImageDims, optimisticCrop, setOptimisticCrop,
  setDiscNoMaskPreview, setDiscCenter, setDiscRadius, setDiscBgColor, setDiscRotation,
  displayToImage, showStatus, showError, handleSaveImage, flushPendingSave, handleLoadImage, handlePasteImage, canSave,
  normalRect, handleNormalCrop, handleUndo,
  unsavedChanges, setUnsavedChanges, confirmClose,
  cornerState, setCornerState, setSelectedCornerPts,
  adjustmentSelectionActive, adjustmentRect, setAdjustmentRect,
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

      if ((e.ctrlKey || e.metaKey) && e.code === 'KeyV') {
        const active = document.activeElement
        if (active && (['INPUT', 'TEXTAREA', 'SELECT'].includes(active.tagName) || active.isContentEditable)) return
        e.preventDefault()
        if (e.repeat) return
        await handlePasteImage()
        return
      }

      if (!imageLoaded) return
      let pendingCrop = null
      try {
        let result

        if ((e.ctrlKey || e.metaKey) && e.code === 'KeyD' && (adjustmentSelectionActive || adjustmentRect)) {
          e.preventDefault()
          if (e.repeat) return
          setAdjustmentRect(null)
          showStatus('Adjustment selection cleared')
          return
        }

        const copyRect = adjustmentRect || (mode === 'normal' ? normalRect : null)
        if ((e.ctrlKey || e.metaKey) && e.code === 'KeyC' && copyRect) {
          const active = document.activeElement
          if (active && (['INPUT', 'TEXTAREA', 'SELECT'].includes(active.tagName) || active.isContentEditable)) return
          e.preventDefault()
          if (e.repeat) return
          showStatus('Copying selection…')
          const message = await CopySelectionToClipboard(adjustmentSelectionPayload(copyRect))
          showStatus(message || 'Selection copied to clipboard')
          return
        }

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
            if (r?.width && r?.height) setRealImageDims({ w: r.width, h: r.height })
            if (r?.unmaskedPreview) setDiscNoMaskPreview(r.unmaskedPreview)
            if (r?.discCenterX !== undefined && r?.discCenterY !== undefined) setDiscCenter({ x: r.discCenterX, y: r.discCenterY })
            if (r?.discRadius !== undefined) setDiscRadius(r.discRadius)
            setDiscRotation(r?.discRotation ?? discRotation)
            if (r?.discBgR !== undefined) setDiscBgColor({ r: r.discBgR, g: r.discBgG, b: r.discBgB })
          }
          switch (e.key) {
            case 'ArrowUp':    e.preventDefault(); await applyShift(0, -shiftStep); return
            case 'ArrowDown':  e.preventDefault(); await applyShift(0,  shiftStep); return
            case 'ArrowLeft':  e.preventDefault(); await applyShift(-shiftStep, 0); return
            case 'ArrowRight': e.preventDefault(); await applyShift( shiftStep, 0); return
            case '+': case '=': {
              const newF = Math.min(100, featherSize + 1); setFeatherSize(newF)
              result = await SetFeatherSize({ size: newF })
              if (result?.preview) setPreview(result.preview)
              if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height })
              return
            }
            case '-': {
              const newF = Math.max(0, featherSize - 1); setFeatherSize(newF)
              result = await SetFeatherSize({ size: newF })
              if (result?.preview) setPreview(result.preview)
              if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height })
              return
            }
            case 'y': case 'Y': {
              const mp = mousePosRef.current
              const imgPt = displayToImage(mp.x, mp.y)
              result = await GetPixelColor({ x: imgPt.x, y: imgPt.y })
              if (result?.preview) setPreview(result.preview)
              if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height })
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
          // In corner mode, undo individual corner clicks before touching the backend undo stack.
          // The 4th click triggers a warp (which pushes to backend undo), so only intercept 1–3.
          if (mode === 'corner' && cornerState.cornerCount > 0 && cornerState.cornerCount < 4) {
            const newCount = cornerState.cornerCount - 1
            await UndoLastCorner()
            setCornerState(s => ({ ...s, cornerCount: newCount }))
            setSelectedCornerPts(prev => prev.slice(0, -1))
            showStatus(newCount === 0
              ? 'Corner selection cleared — click to select corners'
              : `Corner ${newCount} of 4 selected`)
            return
          }
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
          if (['w', 's', 'a', 'd'].includes(key)) e.preventDefault()
          if (!canSave) { showStatus('Apply a crop first before adjusting'); return }
        }

        const beginOptimisticCrop = direction => {
          // Hold a second crop until the first authoritative revision is
          // presented so local geometry and backend undo order stay aligned.
          if (optimisticCrop) return null
          const targetDims = optimisticCropDimensions(realImageDims, direction)
          if (!targetDims || !preview) return null
          pendingCrop = {
            source: preview,
            sourceDims: { ...realImageDims },
            targetDims,
            direction,
            amount: KEYBOARD_CROP_AMOUNT,
          }
          setOptimisticCrop(pendingCrop)
          setRealImageDims(targetDims)
          return pendingCrop
        }

        switch (key) {
          case 'w':
          case 's':
          case 'a':
          case 'd': {
            const direction = { w: 'top', s: 'bottom', a: 'left', d: 'right' }[key]
            if (!beginOptimisticCrop(direction)) return
            result = await Crop({ direction })
            // From this point on the backend state is committed. Keep the
            // optimistic renderer alive until presentation, but do not roll
            // the frontend geometry back if an unrelated deferred save fails.
            pendingCrop = null
            if (result?.preview) setPreview(result.preview)
            if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height })
            setUnsavedChanges(true)
            await flushPendingSave()
            break
          }
          case 'q':
            setLoading(true); showStatus('Rotating…')
            result = mode === 'disc' && discActive
              ? await RotateDisc({ angle: -15 })
              : await Rotate({ flipCode: 2 })
            if (result?.preview) setPreview(result.preview)
            if (result?.unmaskedPreview) setDiscNoMaskPreview(result.unmaskedPreview)
            if (result?.discCenterX !== undefined && result?.discCenterY !== undefined) setDiscCenter({ x: result.discCenterX, y: result.discCenterY })
            if (result?.discRadius !== undefined) setDiscRadius(result.discRadius)
            setDiscRotation(result?.discRotation ?? discRotation)
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
            setDiscRotation(result?.discRotation ?? discRotation)
            if (result?.discBgR !== undefined) setDiscBgColor({ r: result.discBgR, g: result.discBgG, b: result.discBgB })
            if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height })
            setUnsavedChanges(true)
            showStatus(''); setLoading(false); await flushPendingSave(); break
          default:
            break
        }
      } catch (err) {
        console.error('Shortcut error:', err)
        if (pendingCrop) {
          setRealImageDims(pendingCrop.sourceDims)
          setOptimisticCrop(null)
        }
        showError(err)
        setLoading(false)
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [imageLoaded, mode, discActive, featherSize, discRotation, displayToImage, normalRect, handleNormalCrop, handleUndo, canSave, handleLoadImage, handlePasteImage, cornerState.cornerCount, adjustmentSelectionActive, adjustmentRect, preview, realImageDims, optimisticCrop]) // eslint-disable-line react-hooks/exhaustive-deps
}
