// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'

vi.mock('../../wailsjs/go/main/App', () => ({
  GetAllSettings:  vi.fn(),
  SaveAllSettings: vi.fn().mockResolvedValue(undefined),
  SetDiscSettings: vi.fn().mockResolvedValue({}),
}))

import { GetAllSettings, SaveAllSettings, SetDiscSettings } from '../../wailsjs/go/main/App'
import { usePersistentSettings } from './usePersistentSettings'

const MOCK_SET_PREVIEW = vi.fn()

describe('usePersistentSettings – compiled-in defaults', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    // Never-resolving promise so we see the pre-backend state
    GetAllSettings.mockReturnValue(new Promise(() => {}))
  })

  it('returns patchmatch touchup backend by default', () => {
    const { result } = renderHook(() => usePersistentSettings({ setPreview: MOCK_SET_PREVIEW }))
    expect(result.current.touchupBackend).toBe('patchmatch')
  })

  it('returns correct default IOPaint URL', () => {
    const { result } = renderHook(() => usePersistentSettings({ setPreview: MOCK_SET_PREVIEW }))
    expect(result.current.iopaintURL).toBe('http://127.0.0.1:8086/')
  })

  it('returns clamp as default warp fill mode', () => {
    const { result } = renderHook(() => usePersistentSettings({ setPreview: MOCK_SET_PREVIEW }))
    expect(result.current.warpFillMode).toBe('clamp')
  })

  it('disc center cutout is enabled by default', () => {
    const { result } = renderHook(() => usePersistentSettings({ setPreview: MOCK_SET_PREVIEW }))
    expect(result.current.discCenterCutout).toBe(true)
    expect(result.current.discCutoutPercent).toBe(11)
  })

  it('auto-detection settings are enabled by default', () => {
    const { result } = renderHook(() => usePersistentSettings({ setPreview: MOCK_SET_PREVIEW }))
    expect(result.current.autoCornerParams).toBe(true)
    expect(result.current.autoDetectOnModeSwitch).toBe(true)
  })

  it('close-after-save and post-save are disabled by default', () => {
    const { result } = renderHook(() => usePersistentSettings({ setPreview: MOCK_SET_PREVIEW }))
    expect(result.current.closeAfterSave).toBe(false)
    expect(result.current.postSaveEnabled).toBe(false)
    expect(result.current.postSaveCommand).toBe('')
  })

  it('tool-remains-active flags are enabled by default', () => {
    const { result } = renderHook(() => usePersistentSettings({ setPreview: MOCK_SET_PREVIEW }))
    expect(result.current.touchupRemainsActive).toBe(true)
    expect(result.current.straightEdgeRemainsActive).toBe(true)
  })
})

describe('usePersistentSettings – backend merge', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
  })

  it('merges backend values over defaults once GetAllSettings resolves', async () => {
    GetAllSettings.mockResolvedValue({ initialized: true, touchupBackend: 'iopaint', warpFillMode: 'extend' })
    const { result } = renderHook(() => usePersistentSettings({ setPreview: MOCK_SET_PREVIEW }))
    await waitFor(() => expect(result.current.touchupBackend).toBe('iopaint'))
    expect(result.current.warpFillMode).toBe('extend')
    // Keys absent from backend response stay at compiled-in defaults
    expect(result.current.iopaintURL).toBe('http://127.0.0.1:8086/')
  })

  it('does not migrate localStorage when initialized=true', async () => {
    localStorage.setItem('touchupBackend', 'iopaint')
    GetAllSettings.mockResolvedValue({ initialized: true, touchupBackend: 'patchmatch' })
    const { result } = renderHook(() => usePersistentSettings({ setPreview: MOCK_SET_PREVIEW }))
    await waitFor(() => expect(GetAllSettings).toHaveBeenCalled())
    expect(result.current.touchupBackend).toBe('patchmatch')
    expect(SaveAllSettings).not.toHaveBeenCalled()
  })
})

describe('usePersistentSettings – localStorage migration', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    GetAllSettings.mockResolvedValue({ initialized: false })
  })

  it('migrates iopaintURL from localStorage using correct key mapping', async () => {
    // localStorage key is 'iopaintURL'; internal/backend key is 'iopaintUrl'
    localStorage.setItem('iopaintURL', 'http://192.168.1.100:8086/')
    const { result } = renderHook(() => usePersistentSettings({ setPreview: MOCK_SET_PREVIEW }))
    await waitFor(() => expect(result.current.iopaintURL).toBe('http://192.168.1.100:8086/'))
  })

  it('parses boolean localStorage values correctly', async () => {
    localStorage.setItem('closeAfterSave', 'true')
    localStorage.setItem('autoCornerParams', 'false')
    const { result } = renderHook(() => usePersistentSettings({ setPreview: MOCK_SET_PREVIEW }))
    await waitFor(() => expect(result.current.closeAfterSave).toBe(true))
    expect(result.current.autoCornerParams).toBe(false)
  })

  it('parses numeric localStorage values correctly', async () => {
    localStorage.setItem('discCutoutPercent', '25')
    const { result } = renderHook(() => usePersistentSettings({ setPreview: MOCK_SET_PREVIEW }))
    await waitFor(() => expect(result.current.discCutoutPercent).toBe(25))
  })

  it('does not override defaults for localStorage keys that are absent', async () => {
    // Nothing in localStorage; defaults should stand
    const { result } = renderHook(() => usePersistentSettings({ setPreview: MOCK_SET_PREVIEW }))
    await waitFor(() => expect(GetAllSettings).toHaveBeenCalled())
    expect(result.current.warpFillMode).toBe('clamp')
    expect(result.current.discCutoutPercent).toBe(11)
  })

  it('persists migrated settings via SaveAllSettings', async () => {
    localStorage.setItem('closeAfterSave', 'true')
    renderHook(() => usePersistentSettings({ setPreview: MOCK_SET_PREVIEW }))
    await waitFor(() => expect(SaveAllSettings).toHaveBeenCalled())
    const saved = SaveAllSettings.mock.calls[0][0]
    expect(saved.closeAfterSave).toBe(true)
  })
})

describe('usePersistentSettings – setters', () => {
  beforeEach(async () => {
    vi.clearAllMocks()
    localStorage.clear()
    GetAllSettings.mockResolvedValue({ initialized: true })
  })

  it('setTouchupBackend updates state and persists', async () => {
    const { result } = renderHook(() => usePersistentSettings({ setPreview: MOCK_SET_PREVIEW }))
    await waitFor(() => expect(GetAllSettings).toHaveBeenCalled())
    act(() => { result.current.setTouchupBackend('iopaint') })
    expect(result.current.touchupBackend).toBe('iopaint')
    expect(SaveAllSettings).toHaveBeenLastCalledWith(expect.objectContaining({ touchupBackend: 'iopaint' }))
  })

  it('setIopaintURL stores under iopaintUrl key internally', async () => {
    const { result } = renderHook(() => usePersistentSettings({ setPreview: MOCK_SET_PREVIEW }))
    await waitFor(() => expect(GetAllSettings).toHaveBeenCalled())
    act(() => { result.current.setIopaintURL('http://newhost:9090/') })
    expect(result.current.iopaintURL).toBe('http://newhost:9090/')
    expect(SaveAllSettings).toHaveBeenLastCalledWith(
      expect.objectContaining({ iopaintUrl: 'http://newhost:9090/' }),
    )
  })

  it('setDiscCenterCutout persists and calls SetDiscSettings for live re-render', async () => {
    const { result } = renderHook(() => usePersistentSettings({ setPreview: MOCK_SET_PREVIEW }))
    await waitFor(() => expect(GetAllSettings).toHaveBeenCalled())
    act(() => { result.current.setDiscCenterCutout(false) })
    expect(result.current.discCenterCutout).toBe(false)
    expect(SetDiscSettings).toHaveBeenCalledWith(expect.objectContaining({ centerCutout: false }))
  })

  it('SaveAllSettings receives the complete settings object, not just the changed key', async () => {
    GetAllSettings.mockResolvedValue({ initialized: true, warpFillMode: 'extend' })
    const { result } = renderHook(() => usePersistentSettings({ setPreview: MOCK_SET_PREVIEW }))
    await waitFor(() => expect(result.current.warpFillMode).toBe('extend'))
    act(() => { result.current.setCloseAfterSave(true) })
    const saved = SaveAllSettings.mock.calls.at(-1)[0]
    expect(saved.closeAfterSave).toBe(true)
    expect(saved.warpFillMode).toBe('extend') // previous value preserved
    expect(saved.touchupBackend).toBe('patchmatch') // default preserved
  })
})
