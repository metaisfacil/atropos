export function displayToImage(dispX, dispY, realImageDims, clientWidth, clientHeight) {
  if (!realImageDims || realImageDims.w <= 0 || realImageDims.h <= 0) return { x: 0, y: 0 }
  if (clientWidth <= 0 || clientHeight <= 0) return { x: 0, y: 0 }
  return {
    x: Math.round(dispX * (realImageDims.w / clientWidth)),
    y: Math.round(dispY * (realImageDims.h / clientHeight)),
  }
}

export function computeDiscShift(screenDx, screenDy, realImageDims, discRadius, discRotation, discCenter, ctrlStartCenter, clientWidth, clientHeight) {
  if (!realImageDims || realImageDims.w <= 0 || realImageDims.h <= 0 || discRadius <= 0) return null
  if (clientWidth <= 0 || clientHeight <= 0) return null

  const scaleX = realImageDims.w / clientWidth
  const scaleY = realImageDims.h / clientHeight

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

  const previewX = -(cos * appliedImgDx - sin * appliedImgDy)
  const previewY = -(sin * appliedImgDx + cos * appliedImgDy)
  const liveDx = Math.round(previewX / scaleX)
  const liveDy = Math.round(previewY / scaleY)

  return {
    dx: Math.round(appliedImgDx),
    dy: Math.round(appliedImgDy),
    liveDx,
    liveDy,
  }
}
