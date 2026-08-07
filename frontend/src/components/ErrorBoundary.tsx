import { Component, type ErrorInfo, type ReactNode } from 'react'

type Props = { children: ReactNode }
type State = { error: Error | null }

// Renders children inside an error boundary so an unexpected render error
// shows a recoverable card instead of unmounting the whole tree into a blank
// page. The reported message helps users (and us) diagnose the real cause.
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('[alx] 渲染出错：', error, info.componentStack)
  }

  render() {
    if (this.state.error) {
      return (
        <main className="grid min-h-screen place-items-center p-6">
          <section className="grid w-full max-w-md gap-4 rounded-xl border border-red-200 bg-red-50 p-6 text-left shadow-[0_18px_48px_rgb(28_26_23/0.10)] dark:border-red-900 dark:bg-red-950/40">
            <div className="grid gap-1">
              <strong className="text-base font-semibold text-red-800 dark:text-red-200">
                界面遇到问题
              </strong>
              <p className="text-xs leading-5 text-red-700/80 dark:text-red-300/80">
                出现了一次未预期的错误。刷新后如仍复现，请把下方信息反馈给开发者。
              </p>
            </div>
            <pre className="max-h-40 overflow-auto whitespace-pre-wrap break-words rounded-lg border border-red-200 bg-white/60 p-3 text-xs text-red-900 dark:border-red-900 dark:bg-red-950/60 dark:text-red-100">
              {this.state.error.message}
            </pre>
            <footer className="flex justify-end gap-2">
              <button
                className="secondary-button"
                onClick={() => window.location.reload()}
              >
                刷新
              </button>
            </footer>
          </section>
        </main>
      )
    }
    return this.props.children
  }
}
