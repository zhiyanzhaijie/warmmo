import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'

export type ThemePreference = 'system' | 'light' | 'dark'
export type ResolvedTheme = 'light' | 'dark'

interface ThemeContextValue {
  theme: ThemePreference
  resolvedTheme: ResolvedTheme
  setTheme: (theme: ThemePreference) => void
}

interface ThemeProviderProps {
  children: ReactNode
}

const storageKey = 'warmnote-theme'
const ThemeContext = createContext<ThemeContextValue | null>(null)

function getSystemTheme(): ResolvedTheme {
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

function getStoredTheme(): ThemePreference {
  try {
    const storedTheme = window.localStorage.getItem(storageKey)
    return storedTheme === 'light' || storedTheme === 'dark' || storedTheme === 'system'
      ? storedTheme
      : 'system'
  } catch {
    return 'system'
  }
}

export function ThemeProvider({ children }: ThemeProviderProps) {
  const [theme, setThemeState] = useState<ThemePreference>(getStoredTheme)
  const [resolvedTheme, setResolvedTheme] = useState<ResolvedTheme>(() =>
    theme === 'system' ? getSystemTheme() : theme,
  )

  useEffect(() => {
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')

    function applyTheme() {
      const nextResolvedTheme = theme === 'system'
        ? mediaQuery.matches ? 'dark' : 'light'
        : theme
      const root = document.documentElement

      root.classList.remove('light', 'dark')
      root.classList.add(nextResolvedTheme)
      root.dataset.theme = theme
      root.style.colorScheme = nextResolvedTheme
      document.querySelector('meta[name="theme-color"]')?.setAttribute(
        'content',
        nextResolvedTheme === 'dark' ? '#0a0a0a' : '#fafafa',
      )
      setResolvedTheme(nextResolvedTheme)
    }

    applyTheme()
    if (theme !== 'system') {
      return
    }

    mediaQuery.addEventListener('change', applyTheme)
    return () => mediaQuery.removeEventListener('change', applyTheme)
  }, [theme])

  const setTheme = useCallback((nextTheme: ThemePreference) => {
    try {
      window.localStorage.setItem(storageKey, nextTheme)
    } catch {
      // Theme switching remains available for the current session.
    }
    setThemeState(nextTheme)
  }, [])

  const value = useMemo(
    () => ({ theme, resolvedTheme, setTheme }),
    [resolvedTheme, setTheme, theme],
  )

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}

export function useTheme() {
  const context = useContext(ThemeContext)
  if (context === null) {
    throw new Error('useTheme must be used within ThemeProvider')
  }
  return context
}
