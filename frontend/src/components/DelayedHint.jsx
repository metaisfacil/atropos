import React from 'react'
import ReactDOM from 'react-dom'

// DelayedHint: shows a tooltip after hovering for a delay (default 1s),
// rendered in a portal to avoid clipping by overflow:hidden parents.
export default function DelayedHint({ children, hint, delay = 1000, offset = 12 }) {
  const [showHint, setShowHint] = React.useState(false)
  const [hintPos, setHintPos] = React.useState({ top: 0, left: 0 })
  const hintTimeout = React.useRef()
  const childRef = React.useRef()
  const cursorPos = React.useRef({ x: 0, y: 0 })

  const handleMove = (e) => {
    cursorPos.current = { x: e.clientX, y: e.clientY }
  }

  const handleShow = (e) => {
    cursorPos.current = { x: e.clientX, y: e.clientY }
    hintTimeout.current = setTimeout(() => {
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
  }

  const tooltip = showHint
    ? ReactDOM.createPortal(
        <span
          style={{
            position: 'fixed',
            left: hintPos.left,
            top: hintPos.top,
            transform: 'translateY(-50%)',
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
