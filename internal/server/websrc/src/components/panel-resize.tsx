import { useEffect, useRef, useState } from 'react'

const defaults = { left: 290, right: 210 }
const minimums = { left: 220, right: 170 }

export function PanelResize({ side }: { side: 'left' | 'right' }) {
  const handle = useRef<HTMLDivElement>(null)
  const drag = useRef<{ pointer: number; x: number; width: number } | null>(null)
  const minimum = minimums[side]
  const getMaximum = () => side === 'left'
    ? Math.max(minimum, Math.min(440, window.innerWidth * (window.innerWidth > 1180 ? .3 : .4)))
    : Math.max(minimum, Math.min(360, window.innerWidth * .25))
  const [maximum, setMaximum] = useState(getMaximum)
  const [width, setWidth] = useState(() => {
    try {
      const stored = Number(localStorage.getItem(`closeview.panel.${side}`))
      if (Number.isFinite(stored) && stored >= minimum) return Math.min(stored, getMaximum())
    } catch { /* Storage is optional. */ }
    return defaults[side]
  })

  useEffect(() => {
    const onResize = () => {
      const max = getMaximum()
      setMaximum(max)
      setWidth(current => Math.max(minimum, Math.min(current, max)))
      drag.current = null
      document.body.classList.remove('panel-resizing')
    }
    window.addEventListener('resize', onResize)
    return () => {
      window.removeEventListener('resize', onResize)
      document.body.classList.remove('panel-resizing')
    }
  }, [side])

  useEffect(() => {
    const shell = handle.current?.closest<HTMLElement>('.app-shell')
    shell?.style.setProperty(`--panel-${side}`, `${width}px`)
    try { localStorage.setItem(`closeview.panel.${side}`, String(width)) } catch { /* Storage is optional. */ }
  }, [side, width])

  function update(value: number) { setWidth(Math.max(minimum, Math.min(value, getMaximum()))) }
  function finish() {
    drag.current = null
    document.body.classList.remove('panel-resizing')
  }

  return <div
    ref={handle}
    className={`panel-resize panel-resize-${side}`}
    role="separator"
    tabIndex={0}
    aria-label={`Resize ${side === 'left' ? 'sessions' : 'prompts'} panel`}
    aria-controls={side === 'left' ? 'sessions-panel' : 'prompts-panel'}
    aria-orientation="vertical"
    aria-valuemin={minimum}
    aria-valuemax={Math.round(maximum)}
    aria-valuenow={Math.round(width)}
    aria-valuetext={`${Math.round(width)} pixels`}
    title="Drag to resize · Arrow keys to adjust · Double-click to reset"
    onPointerDown={event => {
      if (!event.isPrimary || event.button !== 0 || drag.current) return
      event.preventDefault()
      event.currentTarget.focus()
      event.currentTarget.setPointerCapture(event.pointerId)
      drag.current = { pointer: event.pointerId, x: event.clientX, width: event.currentTarget.parentElement?.getBoundingClientRect().width ?? width }
      document.body.classList.add('panel-resizing')
    }}
    onPointerMove={event => {
      const active = drag.current
      if (!active || active.pointer !== event.pointerId) return
      update(active.width + (event.clientX - active.x) * (side === 'left' ? 1 : -1))
    }}
    onPointerUp={event => {
      if (drag.current?.pointer !== event.pointerId) return
      finish()
      event.currentTarget.releasePointerCapture(event.pointerId)
    }}
    onPointerCancel={finish}
    onLostPointerCapture={finish}
    onDoubleClick={() => update(defaults[side])}
    onKeyDown={event => {
      if (event.key === 'Home') update(minimum)
      else if (event.key === 'End') update(maximum)
      else if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
        update(width + (event.key === 'ArrowRight' ? 1 : -1) * (side === 'left' ? 1 : -1) * (event.shiftKey ? 40 : 10))
      } else return
      event.preventDefault()
    }}
  />
}
