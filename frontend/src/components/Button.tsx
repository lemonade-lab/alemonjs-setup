import cn from 'classnames'
import type { ButtonHTMLAttributes, ReactNode } from 'react'

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger' | 'icon'
export type ButtonSize = 'sm' | 'md' | 'icon'

const variantClasses: Record<ButtonVariant, string> = {
  primary: 'primary-button',
  secondary: 'secondary-button',
  ghost: 'text-button',
  danger: 'danger-button',
  icon: 'icon-button'
}

const sizeClasses: Record<ButtonSize, string> = {
  sm: '',
  md: 'min-h-9',
  icon: 'size-8 p-0'
}

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant
  size?: ButtonSize
  loading?: boolean
  loadingLabel?: ReactNode
}

/**
 * The shared button primitive for the workspace.  Variants intentionally map
 * to the existing theme classes so legacy controls and new controls keep one
 * visual contract while the UI is migrated incrementally.
 */
export function Button({
  variant = 'secondary',
  size = variant === 'icon' ? 'icon' : 'sm',
  loading = false,
  loadingLabel = '处理中…',
  className,
  children,
  disabled,
  ...props
}: ButtonProps) {
  return (
    <button
      {...props}
      className={cn(variantClasses[variant], sizeClasses[size], className)}
      disabled={disabled || loading}
    >
      {loading ? loadingLabel : children}
    </button>
  )
}
