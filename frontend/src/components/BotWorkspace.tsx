import type { ReactNode } from 'react'

type Props = {
  className?: string
  header: ReactNode
  children: ReactNode
}

/** A robot page always has sibling header and article regions. */
export function BotWorkspace({ className = '', header, children }: Props) {
  return (
    <section className={`bot-workspace ${className}`.trim()}>
      {header}
      <article className="bot-workspace-article">{children}</article>
    </section>
  )
}
