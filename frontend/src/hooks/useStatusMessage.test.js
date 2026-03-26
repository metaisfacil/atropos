// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useStatusMessage } from './useStatusMessage'

describe('useStatusMessage', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })

  it('starts with empty message and visible state', () => {
    const { result } = renderHook(() => useStatusMessage())
    expect(result.current.imageInfo).toBe('')
    expect(result.current.imageInfoVisible).toBe(true)
  })

  it('showStatus sets message and ensures visibility', () => {
    const { result } = renderHook(() => useStatusMessage())
    act(() => { result.current.showStatus('Test message') })
    expect(result.current.imageInfo).toBe('Test message')
    expect(result.current.imageInfoVisible).toBe(true)
  })

  it('message fades after 4 seconds but text remains', () => {
    const { result } = renderHook(() => useStatusMessage())
    act(() => { result.current.showStatus('Fading message') })
    act(() => { vi.advanceTimersByTime(4000) })
    expect(result.current.imageInfoVisible).toBe(false)
    expect(result.current.imageInfo).toBe('Fading message')
  })

  it('message text is cleared after 5 seconds', () => {
    const { result } = renderHook(() => useStatusMessage())
    act(() => { result.current.showStatus('Temporary') })
    act(() => { vi.advanceTimersByTime(5000) })
    expect(result.current.imageInfo).toBe('')
  })

  it('empty-string message clears immediately without starting timers', () => {
    const { result } = renderHook(() => useStatusMessage())
    act(() => { result.current.showStatus('') })
    expect(result.current.imageInfo).toBe('')
    expect(result.current.imageInfoVisible).toBe(true)
    act(() => { vi.advanceTimersByTime(5000) })
    // State should not have changed
    expect(result.current.imageInfo).toBe('')
    expect(result.current.imageInfoVisible).toBe(true)
  })

  it('rapid calls cancel previous timers so only latest timer fires', () => {
    const { result } = renderHook(() => useStatusMessage())
    act(() => { result.current.showStatus('First') })
    // Advance close to the 4s threshold but not past it
    act(() => { vi.advanceTimersByTime(3500) })
    // Update with a new message — this resets both timers
    act(() => { result.current.showStatus('Second') })
    // Advance 4000ms past Second's showStatus call to trigger Second's fade timer
    act(() => { vi.advanceTimersByTime(4000) })
    // First's timer was cancelled; Second's 4s fade timer fires.
    expect(result.current.imageInfoVisible).toBe(false)
    expect(result.current.imageInfo).toBe('Second')
  })

  it('showStatus after fade restores visibility', () => {
    const { result } = renderHook(() => useStatusMessage())
    act(() => { result.current.showStatus('First') })
    act(() => { vi.advanceTimersByTime(4000) })
    expect(result.current.imageInfoVisible).toBe(false)
    // Show a new message — visibility should come back
    act(() => { result.current.showStatus('Second') })
    expect(result.current.imageInfoVisible).toBe(true)
    expect(result.current.imageInfo).toBe('Second')
  })
})
