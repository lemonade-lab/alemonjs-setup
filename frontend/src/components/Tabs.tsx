import cn from 'classnames'
import type { ReactNode } from 'react'

export type TabItem<T extends string> = {
  id: T
  label: ReactNode
  icon?: ReactNode
  meta?: ReactNode
  disabled?: boolean
}

type TabsProps<T extends string> = {
  items: readonly TabItem<T>[]
  value: T
  onChange: (value: T) => void
  ariaLabel: string
  variant?: 'underline' | 'segmented' | 'pill'
  className?: string
}

/** Shared accessible tab switcher for workspace panels and dialogs. */
export function Tabs<T extends string>({
  items,
  value,
  onChange,
  ariaLabel,
  variant = 'underline',
  className
}: TabsProps<T>) {
  return (
    <div
      className={cn('ui-tabs', `ui-tabs--${variant}`, className)}
      role="tablist"
      aria-label={ariaLabel}
    >
      {items.map(item => {
        const active = value === item.id
        return (
          <button
            className={cn('ui-tab', active && 'ui-tab--active')}
            key={item.id}
            role="tab"
            aria-selected={active}
            disabled={item.disabled}
            onClick={() => onChange(item.id)}
          >
            {item.icon}
            <span>{item.label}</span>
            {item.meta && <small>{item.meta}</small>}
          </button>
        )
      })}
    </div>
  )
}
