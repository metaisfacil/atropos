import React from 'react'
import ReactDOM from 'react-dom'

// DelayedHint: shows a tooltip after hovering for a delay (default 1s),
// rendered in a portal to avoid clipping by overflow:hidden parents.
// The tooltip is clamped to the viewport so it is never partially off-screen.
export default function DelayedHint({ children, hint, delay = 1000, offset = 12 }) {
  const [showHint, setShowHint] = React.useState(false)
  const [hintPos, setHintPos] = React.useState({ top: 0, left: 0 })
  const [tooltipVisible, setTooltipVisible] = React.useState(false)
  const hintTimeout = React.useRef()
  const childRef = React.useRef()
  const tooltipRef = React.useRef()
  const cursorPos = React.useRef({ x: 0, y: 0 })

  const handleMove = (e) => {
    cursorPos.current = { x: e.clientX, y: e.clientY }
  }

  const handleShow = (e) => {
    cursorPos.current = { x: e.clientX, y: e.clientY }
    hintTimeout.current = setTimeout(() => {
      setTooltipVisible(false)
      setHintPos({
        top: cursorPos.current.y,
        left: cursorPos.current.x + offset,
      })
      setShowHint(true)
    }, delay)
  }

  const handleHide = () => {
    clearTimeout(hintTimeout.current)
    setShowHint(false)
    setTooltipVisible(false)
  }

  // After each render where the tooltip is shown, measure it and clamp to viewport.
  // The tooltip renders with visibility:hidden first, then this effect reveals it.
  React.useLayoutEffect(() => {
    if (!showHint || !tooltipRef.current) return

    const el = tooltipRef.current
    const rect = el.getBoundingClientRect()
    const margin = 8
    const vw = window.innerWidth
    const vh = window.innerHeight

    // translateY(-50%) means the tooltip is vertically centred on hintPos.top
    let { top, left } = hintPos

    // Horizontal: prefer right of cursor; flip left if it overflows
    if (left + rect.width + margin > vw) {
      left = cursorPos.current.x - offset - rect.width
    }
    left = Math.max(margin, Math.min(left, vw - rect.width - margin))

    // Vertical: clamp so the centred tooltip stays within the viewport
    const halfH = rect.height / 2
    top = Math.max(margin + halfH, Math.min(top, vh - margin - halfH))

    if (top !== hintPos.top || left !== hintPos.left) {
      setHintPos({ top, left })
      // Visibility is set on the next layout effect pass (position already correct)
      return
    }

    setTooltipVisible(true)
  }, [showHint, hintPos]) // eslint-disable-line react-hooks/exhaustive-deps

  const tooltip = showHint
    ? ReactDOM.createPortal(
        <span
          ref={tooltipRef}
          style={{
            position: 'fixed',
            left: hintPos.left,
            top: hintPos.top,
            transform: 'translateY(-50%)',
            visibility: tooltipVisible ? 'visible' : 'hidden',
            background: '#222',
            color: '#fff',
            fontSize: 13,
            borderRadius: 4,
            padding: '4px 10px',
            maxWidth: 280,
            whiteSpace: 'normal',
            wordBreak: 'break-word',
            zIndex: 9999,
            boxShadow: '0 2px 8px rgba(0,0,0,0.15)',
            lineHeight: 1.4,
          }}
        >
          {hint}
        </span>,
        document.body
      )
    : null

  const child = React.Children.only(children)
  const childWithProps = React.cloneElement(child, {
    ref: childRef,
    onMouseEnter: handleShow,
    onMouseLeave: handleHide,
    onMouseMove: handleMove,
    onBlur: handleHide,
    tabIndex: child.props.tabIndex || 0,
  })

  return (
    <>
      {childWithProps}
      {tooltip}
    </>
  )
}
