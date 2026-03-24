import { addDebugLog, initFrontendDebugLogAPI } from './debugLogger'

initFrontendDebugLogAPI()

export function getDisplayScale(realImageDims, clientWidth, clientHeight, naturalImageDims = null) {
  if (!realImageDims || realImageDims.w <= 0 || realImageDims.h <= 0) return null
  if (clientWidth <= 0 || clientHeight <= 0) return null

  const source = (naturalImageDims && naturalImageDims.w > 0 && naturalImageDims.h > 0)
    ? naturalImageDims
    : realImageDims

  return {
    scaleX: source.w / clientWidth,
    scaleY: source.h / clientHeight,
  }
}

export function displayToImage(dispX, dispY, realImageDims, clientWidth, clientHeight, naturalImageDims = null) {
  if (!realImageDims || realImageDims.w <= 0 || realImageDims.h <= 0) return { x: 0, y: 0 }
  if (clientWidth <= 0 || clientHeight <= 0) return { x: 0, y: 0 }

  const scale = getDisplayScale(realImageDims, clientWidth, clientHeight, naturalImageDims)
  if (!scale) return { x: 0, y: 0 }

  return {
    x: Math.round(dispX * scale.scaleX),
    y: Math.round(dispY * scale.scaleY),
  }
}

export function computeDiscShift(
  screenDx,
  screenDy,
  realImageDims,
  discRadius,
  discRotation,
  discCenter,
  ctrlStartCenter,
  clientWidth,
  clientHeight,
  naturalImageDims = null,
) {
  // Core invariants:
  //  - `realImageDims` is full source image dims (used for center/clamping boundaries)
  //  - `naturalImageDims` is display image dims (used for live-scale conversion during disc drag)
  //  - `screenDx/screenDy` are pointer movement in CSS pixels at current viewport
  //  - `discRotation` is the current disc rotation angle in degrees
  //  - `ctrlStartCenter` is the center where the current ctrl-drag began
  //  - Returned `dx/dy` are integer image-space shifts for backend ShiftDisc
  //  - Returned `liveDx/liveDy` are display-space preview offsets for immediate drag UX

  // DON’T use realImageDims for live scale in disc mode; pass naturalImageDims explicitly.
  // Disc mode may display a cropped or transformed preview image that has different
  // pixel dimensions from the full source image, so scale must be computed from
  // the natural dimensions of the displayed img element when available.

  if (!realImageDims || realImageDims.w <= 0 || realImageDims.h <= 0 || discRadius <= 0) return null
  if (clientWidth <= 0 || clientHeight <= 0) return null

  const scale = getDisplayScale(realImageDims, clientWidth, clientHeight, naturalImageDims)
  if (!scale) return null

  const scaleX = scale.scaleX
  const scaleY = scale.scaleY

  const rotatedX = screenDx * scaleX
  const rotatedY = screenDy * scaleY
  const angleRad = discRotation * Math.PI / 180
  const cos = Math.cos(angleRad)
  const sin = Math.sin(angleRad)

  const desiredImgDx = -(rotatedX * cos + rotatedY * sin)
  const desiredImgDy = -(-rotatedX * sin + rotatedY * cos)

  const startCenter = ctrlStartCenter || discCenter || { x: 0, y: 0 }
  const minCenterX = discRadius
  const maxCenterX = Math.max(discRadius, realImageDims.w - discRadius)
  const minCenterY = discRadius
  const maxCenterY = Math.max(discRadius, realImageDims.h - discRadius)

  const clampedCenterX = Math.min(Math.max(startCenter.x + desiredImgDx, minCenterX), maxCenterX)
  const clampedCenterY = Math.min(Math.max(startCenter.y + desiredImgDy, minCenterY), maxCenterY)
  const appliedImgDx = clampedCenterX - startCenter.x
  const appliedImgDy = clampedCenterY - startCenter.y

  const roundedImgDx = Math.round(appliedImgDx)
  const roundedImgDy = Math.round(appliedImgDy)

  const previewX = -(cos * roundedImgDx - sin * roundedImgDy)
  const previewY = -(sin * roundedImgDx + cos * roundedImgDy)

  const liveDx = Math.round(previewX / scaleX)
  const liveDy = Math.round(previewY / scaleY)

  const result = {
    dx: roundedImgDx,
    dy: roundedImgDy,
    liveDx,
    liveDy,
  }

  const expectedLiveDx = Math.round(previewX / scaleX)
  const expectedLiveDy = Math.round(previewY / scaleY)

  const message = {
    screenDx, screenDy,
    realImageDims, discRadius, discRotation, discCenter, ctrlStartCenter,
    scaleX, scaleY, rotatedX, rotatedY, desiredImgDx, desiredImgDy,
    appliedImgDx, appliedImgDy,
    roundedImgDx, roundedImgDy,
    liveDx, liveDy,
    expectedLiveDx, expectedLiveDy,
    match: liveDx === expectedLiveDx && liveDy === expectedLiveDy,
  }

  console.debug('computeDiscShift', message)
  addDebugLog('computeDiscShift', message)

  return result
}
