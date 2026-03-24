import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'

export default function OptionsTabs({ tabs, activeTab, onChange }) {
  const shellRef = useRef(null)
  const panelRef = useRef(null)
  const [renderedTab, setRenderedTab] = useState(activeTab)
  const [panelVisible, setPanelVisible] = useState(true)

  const renderedContent = useMemo(
    () => tabs.find((tab) => tab.id === renderedTab)?.content ?? null,
    [tabs, renderedTab]
  )

  useEffect(() => {
    if (activeTab === renderedTab) return
    setPanelVisible(false)
    setRenderedTab(activeTab)
  }, [activeTab, renderedTab])

  useLayoutEffect(() => {
    const shell = shellRef.current
    const panel = panelRef.current
    if (!shell || !panel) return

    shell.style.height = `${panel.scrollHeight}px`
    const raf = requestAnimationFrame(() => setPanelVisible(true))
    return () => cancelAnimationFrame(raf)
  }, [renderedTab, renderedContent])

  useEffect(() => {
    const shell = shellRef.current
    const panel = panelRef.current
    if (!shell || !panel || typeof ResizeObserver === 'undefined') return

    const ro = new ResizeObserver(() => {
      shell.style.height = `${panel.scrollHeight}px`
    })

    ro.observe(panel)
    return () => ro.disconnect()
  }, [renderedTab])

  return (
    <div className="options-tabs">
      <div
        className="options-tablist"
        role="tablist"
        aria-orientation="vertical"
        aria-label="Options categories"
      >
        {tabs.map((tab) => {
          const selected = tab.id === activeTab
          return (
            <button
              key={tab.id}
              id={`options-tab-${tab.id}`}
              type="button"
              role="tab"
              aria-selected={selected}
              aria-controls={`options-panel-${tab.id}`}
              tabIndex={selected ? 0 : -1}
              className={`options-tab ${selected ? 'active' : ''}`}
              onClick={() => onChange(tab.id)}
            >
              {tab.label}
            </button>
          )
        })}
      </div>

      <div ref={shellRef} className="options-tab-shell">
        <div
          ref={panelRef}
          id={`options-panel-${renderedTab}`}
          role="tabpanel"
          aria-labelledby={`options-tab-${renderedTab}`}
          className={`options-tabpanel ${panelVisible ? 'visible' : ''}`}
        >
          {renderedContent}
        </div>
      </div>
    </div>
  )
}