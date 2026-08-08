import { useCallback, useEffect, useRef } from 'react'

/**
 * Schedules a write after the user pauses input. Initial data hydration never
 * calls this hook, so opening a form cannot overwrite a robot's config.
 */
export function useAutoSave<T>(
  save: (value: T) => void | Promise<unknown>,
  delay = 500
) {
  const saveRef = useRef(save)
  const timerRef = useRef<number | null>(null)

  useEffect(() => {
    saveRef.current = save
  }, [save])
  useEffect(
    () => () => {
      if (timerRef.current !== null) window.clearTimeout(timerRef.current)
    },
    []
  )

  return useCallback(
    (value: T) => {
      if (timerRef.current !== null) window.clearTimeout(timerRef.current)
      timerRef.current = window.setTimeout(() => {
        timerRef.current = null
        void saveRef.current(value)
      }, delay)
    },
    [delay]
  )
}
