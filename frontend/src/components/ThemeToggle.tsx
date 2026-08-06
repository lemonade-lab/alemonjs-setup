import { Moon, Sun } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Button } from './Button'

type Theme = 'light' | 'dark'
const key = 'alemonjs-theme'

function preferredTheme(): Theme {
  const saved = window.localStorage.getItem(key)
  if (saved === 'light' || saved === 'dark') return saved
  return window.matchMedia('(prefers-color-scheme: dark)').matches
    ? 'dark'
    : 'light'
}

export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(() => preferredTheme())
  useEffect(() => {
    document.documentElement.dataset.theme = theme
    document.documentElement.style.colorScheme = theme
    window.localStorage.setItem(key, theme)
  }, [theme])
  const next = theme === 'dark' ? 'light' : 'dark'
  return (
    <Button
      variant="icon"
      onClick={() => setTheme(next)}
      aria-label={`切换到${next === 'dark' ? '暗色' : '亮色'}主题`}
      title={`切换到${next === 'dark' ? '暗色' : '亮色'}主题`}
    >
      {theme === 'dark' ? (
        <Sun className="size-4" />
      ) : (
        <Moon className="size-4" />
      )}
    </Button>
  )
}
