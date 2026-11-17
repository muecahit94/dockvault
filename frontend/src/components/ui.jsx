import { Children, isValidElement, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'
import { X, CheckCircle2, AlertCircle, Info, ChevronDown, Check } from 'lucide-react'

function cn(...inputs) {
  return twMerge(clsx(inputs))
}

export function Card({ className, children, ...props }) {
  return (
    <div
      style={{
        backgroundColor: 'var(--dv-surface)',
        borderColor: 'var(--dv-border)',
      }}
      className={cn('rounded-xl border shadow-sm backdrop-blur-sm', className)}
      {...props}
    >
      {children}
    </div>
  )
}

export function Button({
  className,
  variant = 'primary',
  size = 'md',
  disabled,
  style,
  children,
  ...props
}) {
  const variants = {
    primary: 'font-medium shadow-sm hover:opacity-90',
    secondary: 'border hover:opacity-90',
    outline: 'border bg-transparent hover:bg-zinc-800/80',
    ghost: 'bg-transparent hover:bg-zinc-800/80',
    danger: 'bg-rose-500/15 text-rose-300 border border-rose-500/30 hover:bg-rose-500/25',
  }

  const variantStyle =
    variant === 'primary'
      ? { backgroundColor: 'var(--dv-accent)', color: 'var(--dv-accent-fg)' }
      : variant === 'secondary'
        ? {
            backgroundColor: 'var(--dv-surface-alt)',
            borderColor: 'var(--dv-border-strong)',
            color: 'var(--dv-text)',
          }
        : variant === 'outline'
          ? {
              borderColor: 'var(--dv-border-strong)',
              color: 'var(--dv-text-secondary)',
            }
          : variant === 'ghost'
            ? { color: 'var(--dv-text-muted)' }
            : undefined

  const sizes = {
    sm: 'h-8 px-2.5 text-xs rounded-lg gap-1.5',
    md: 'h-9 px-3.5 text-sm rounded-lg gap-2',
    lg: 'h-10 px-4 text-sm rounded-lg gap-2',
  }

  return (
    <button
      disabled={disabled}
      style={{ ...variantStyle, ...style }}
      className={cn(
        'inline-flex items-center justify-center transition-all cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed',
        variants[variant],
        sizes[size],
        className,
      )}
      {...props}
    >
      {children}
    </button>
  )
}

export function Badge({ className, variant = 'neutral', style, children }) {
  const variants = {
    neutral: 'bg-zinc-800 text-zinc-300 border-zinc-700',
    success: 'bg-emerald-500/10 text-emerald-300 border-emerald-500/30',
    warning: 'bg-amber-500/10 text-amber-300 border-amber-500/30',
    danger: 'bg-rose-500/10 text-rose-300 border-rose-500/30',
    info: '',
  }
  const infoStyle =
    variant === 'info'
      ? {
          backgroundColor: 'var(--dv-accent-soft)',
          borderColor: 'var(--dv-accent-border)',
          color: 'var(--dv-accent-text)',
        }
      : undefined
  return (
    <span
      style={{ ...infoStyle, ...style }}
      className={cn(
        'inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-medium',
        variants[variant],
        className,
      )}
    >
      {children}
    </span>
  )
}

export function Input({ className, style, ...props }) {
  return (
    <input
      style={{
        backgroundColor: 'var(--dv-input-bg)',
        borderColor: 'var(--dv-border-strong)',
        color: 'var(--dv-text)',
        ...style,
      }}
      className={cn(
        'w-full rounded-lg border px-3 py-2 text-sm placeholder:text-zinc-500 focus:outline-none focus:ring-2 focus:ring-sky-500/40',
        className,
      )}
      {...props}
    />
  )
}

function extractOptionLabel(node) {
  if (node === null || node === undefined || typeof node === 'boolean') return ''
  if (typeof node === 'string' || typeof node === 'number') return String(node)
  if (Array.isArray(node)) return node.map(extractOptionLabel).join('')
  if (isValidElement(node)) return extractOptionLabel(node.props?.children)
  return ''
}

function extractSelectOptions(children) {
  const result = []
  Children.forEach(children, (child) => {
    if (!isValidElement(child)) return
    if (child.type === 'option') {
      const label = extractOptionLabel(child.props?.children)
      const rawValue = child.props?.value !== undefined ? child.props.value : label
      result.push({
        value: String(rawValue),
        label,
        disabled: Boolean(child.props?.disabled),
      })
      return
    }
    if (child.props?.children) {
      result.push(...extractSelectOptions(child.props.children))
    }
  })
  return result
}

export function Select({
  className,
  style,
  children,
  value,
  onChange,
  disabled,
  id,
  placeholder,
  ...props
}) {
  const options = useMemo(() => extractSelectOptions(children), [children])
  const selectedValue = value !== undefined && value !== null ? String(value) : ''
  const selectedIndex = options.findIndex((opt) => opt.value === selectedValue)
  const selectedOption = selectedIndex >= 0 ? options[selectedIndex] : options[0]

  const [open, setOpen] = useState(false)
  const [activeIndex, setActiveIndex] = useState(-1)
  const [menuPos, setMenuPos] = useState(null)

  const triggerRef = useRef(null)
  const menuRef = useRef(null)

  const updatePosition = useCallback(() => {
    const el = triggerRef.current
    if (!el) return
    const rect = el.getBoundingClientRect()
    const spaceBelow = window.innerHeight - rect.bottom
    const spaceAbove = rect.top
    const estimatedHeight = Math.min(options.length * 36 + 12, 248)
    const openUpward = spaceBelow < estimatedHeight && spaceAbove > spaceBelow
    const available = openUpward ? spaceAbove : spaceBelow
    const left = Math.max(8, Math.min(rect.left, window.innerWidth - rect.width - 8))

    setMenuPos({
      left,
      width: rect.width,
      top: openUpward ? undefined : rect.bottom + 6,
      bottom: openUpward ? window.innerHeight - rect.top + 6 : undefined,
      maxHeight: Math.max(120, Math.min(248, available - 14)),
    })
  }, [options.length])

  const closeMenu = useCallback(() => {
    setOpen(false)
    setActiveIndex(-1)
  }, [])

  const selectOption = useCallback(
    (opt) => {
      if (!opt || opt.disabled) return
      onChange?.({ target: { value: opt.value } })
      closeMenu()
      triggerRef.current?.focus()
    },
    [onChange, closeMenu],
  )

  const openMenu = useCallback(() => {
    if (disabled || options.length === 0) return
    updatePosition()
    setActiveIndex(selectedIndex >= 0 ? selectedIndex : 0)
    setOpen(true)
  }, [disabled, options.length, selectedIndex, updatePosition])

  useEffect(() => {
    if (!open) return undefined

    const handlePointerDown = (e) => {
      if (triggerRef.current?.contains(e.target) || menuRef.current?.contains(e.target)) {
        return
      }
      closeMenu()
    }

    const handleScroll = (e) => {
      if (menuRef.current?.contains(e.target)) return
      updatePosition()
    }

    const handleResize = () => updatePosition()

    const handleKeyDown = (e) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        closeMenu()
        triggerRef.current?.focus()
      }
    }

    document.addEventListener('mousedown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)
    window.addEventListener('scroll', handleScroll, true)
    window.addEventListener('resize', handleResize)

    return () => {
      document.removeEventListener('mousedown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
      window.removeEventListener('scroll', handleScroll, true)
      window.removeEventListener('resize', handleResize)
    }
  }, [open, closeMenu, updatePosition])

  const moveActive = (delta) => {
    if (options.length === 0) return
    let next = activeIndex < 0 ? 0 : activeIndex
    for (let i = 0; i < options.length; i += 1) {
      next = (next + delta + options.length) % options.length
      if (!options[next].disabled) {
        setActiveIndex(next)
        break
      }
    }
  }

  const handleTriggerKeyDown = (e) => {
    if (disabled) return
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      if (!open) openMenu()
      else moveActive(1)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      if (!open) openMenu()
      else moveActive(-1)
    } else if (e.key === 'Enter' || e.key === ' ') {
      if (open) {
        e.preventDefault()
        if (activeIndex >= 0 && options[activeIndex]) {
          selectOption(options[activeIndex])
        } else {
          closeMenu()
        }
      }
    }
  }

  return (
    <>
      <button
        ref={triggerRef}
        id={id}
        type="button"
        disabled={disabled}
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => (open ? closeMenu() : openMenu())}
        onKeyDown={handleTriggerKeyDown}
        style={{
          backgroundColor: 'var(--dv-input-bg)',
          borderColor: open ? 'var(--dv-accent-border)' : 'var(--dv-border-strong)',
          color: 'var(--dv-text)',
          ...style,
        }}
        className={cn(
          'w-full inline-flex items-center justify-between gap-2 rounded-lg border px-3 py-2 text-sm leading-5 text-left transition-colors cursor-pointer focus:outline-none focus:ring-2 focus:ring-sky-500/40 disabled:opacity-50 disabled:cursor-not-allowed',
          className,
        )}
        {...props}
      >
        <span className="truncate">{selectedOption ? selectedOption.label : placeholder || ''}</span>
        <ChevronDown
          className={cn('h-4 w-4 shrink-0 transition-transform duration-150', open && 'rotate-180')}
          style={{ color: 'var(--dv-text-subtle)' }}
        />
      </button>

      {open &&
        menuPos &&
        createPortal(
          <div
            ref={menuRef}
            role="listbox"
            aria-labelledby={id}
            style={{
              position: 'fixed',
              left: menuPos.left,
              top: menuPos.top,
              bottom: menuPos.bottom,
              minWidth: menuPos.width,
              maxWidth: 'min(92vw, 440px)',
              maxHeight: menuPos.maxHeight,
              zIndex: 9999,
              backgroundColor: 'var(--dv-surface-solid)',
              borderColor: 'var(--dv-border-strong)',
              color: 'var(--dv-text)',
            }}
            className="overflow-y-auto rounded-xl border p-1 shadow-2xl"
          >
            {options.map((opt, idx) => {
              const isSelected = Boolean(selectedOption && opt.value === selectedOption.value)
              const isActive = idx === activeIndex
              return (
                <button
                  key={`${opt.value}-${idx}`}
                  type="button"
                  role="option"
                  aria-selected={isSelected}
                  disabled={opt.disabled}
                  onMouseEnter={() => setActiveIndex(idx)}
                  onMouseLeave={() => setActiveIndex(-1)}
                  onClick={() => selectOption(opt)}
                  style={{
                    backgroundColor: isSelected
                      ? 'var(--dv-accent-soft)'
                      : isActive
                        ? 'var(--dv-surface-hover)'
                        : 'transparent',
                    color: isSelected ? 'var(--dv-accent-text)' : 'var(--dv-text)',
                  }}
                  className={cn(
                    'flex w-full items-center justify-between gap-2.5 rounded-lg px-2.5 py-2 text-left text-sm transition-colors cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed',
                    isSelected && 'font-medium',
                  )}
                >
                  <span className="truncate">{opt.label}</span>
                  <Check
                    className={cn(
                      'h-3.5 w-3.5 shrink-0 transition-opacity',
                      isSelected ? 'opacity-100' : 'opacity-0',
                    )}
                  />
                </button>
              )
            })}
          </div>,
          document.body,
        )}
    </>
  )
}

export function Label({ className, children, ...props }) {
  return (
    <label className={cn('block text-xs font-medium text-zinc-300 mb-1.5', className)} {...props}>
      {children}
    </label>
  )
}

export function Switch({ checked, onChange, disabled, id }) {
  return (
    <button
      id={id}
      type="button"
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      style={{
        backgroundColor: checked ? 'var(--dv-accent)' : 'var(--dv-switch-off)',
      }}
      className="relative inline-flex h-5 w-9 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors disabled:opacity-50"
    >
      <span
        className={cn(
          'pointer-events-none inline-block h-4 w-4 rounded-full bg-white shadow transition-transform',
          checked ? 'translate-x-4' : 'translate-x-0',
        )}
      />
    </button>
  )
}

export function Modal({ open, onClose, title, subtitle, children, maxWidth = 'max-w-lg' }) {
  if (!open) return null
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="fixed inset-0 bg-black/60 backdrop-blur-xs" onClick={onClose} />
      <div
        style={{
          backgroundColor: 'var(--dv-surface-solid)',
          borderColor: 'var(--dv-border)',
          color: 'var(--dv-text)',
        }}
        className={cn('relative z-10 w-full rounded-2xl border shadow-2xl', maxWidth)}
      >
        <div
          style={{ borderColor: 'var(--dv-border)' }}
          className="flex items-start justify-between border-b px-5 py-4"
        >
          <div>
            <h3 className="text-base font-semibold text-zinc-100">{title}</h3>
            {subtitle && <p className="mt-0.5 text-xs text-zinc-400">{subtitle}</p>}
          </div>
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg p-1 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100 cursor-pointer"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="p-5">{children}</div>
      </div>
    </div>
  )
}

export function EmptyState({ icon: Icon, title, description, action }) {
  return (
    <div
      style={{ borderColor: 'var(--dv-border-strong)' }}
      className="flex flex-col items-center justify-center rounded-xl border border-dashed bg-zinc-900/30 px-6 py-12 text-center"
    >
      {Icon && (
        <div
          style={{ borderColor: 'var(--dv-border)' }}
          className="mb-3 rounded-xl border bg-zinc-900 p-3 text-zinc-400"
        >
          <Icon className="h-6 w-6" />
        </div>
      )}
      <h4 className="text-sm font-medium text-zinc-200">{title}</h4>
      {description && <p className="mt-1 max-w-md text-xs text-zinc-400">{description}</p>}
      {action && <div className="mt-4">{action}</div>}
    </div>
  )
}

export function ToastContainer({ toasts, onDismiss }) {
  if (!toasts.length) return null
  return (
    <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2 max-w-sm w-full pointer-events-none">
      {toasts.map((toast) => {
        const iconMap = {
          success: <CheckCircle2 className="h-4 w-4 text-emerald-400 shrink-0" />,
          error: <AlertCircle className="h-4 w-4 text-rose-400 shrink-0" />,
          info: <Info className="h-4 w-4 text-sky-400 shrink-0" />,
        }
        return (
          <div
            key={toast.id}
            style={{
              backgroundColor: 'var(--dv-surface-solid)',
              borderColor: 'var(--dv-border-strong)',
              color: 'var(--dv-text)',
            }}
            className="pointer-events-auto flex items-start gap-2.5 rounded-xl border px-3.5 py-3 text-sm shadow-xl backdrop-blur"
          >
            <div className="mt-0.5">{iconMap[toast.type || 'info']}</div>
            <div className="flex-1 text-xs leading-relaxed">{toast.message}</div>
            <button
              type="button"
              onClick={() => onDismiss(toast.id)}
              className="text-zinc-500 hover:text-zinc-200 cursor-pointer"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </div>
        )
      })}
    </div>
  )
}
