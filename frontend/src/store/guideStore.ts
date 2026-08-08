import {
  configureStore,
  createSlice,
  type PayloadAction
} from '@reduxjs/toolkit'
import { persistReducer, persistStore } from 'redux-persist'
import { useCallback, useEffect, useId, useMemo } from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { workspaceApi } from './workspaceApi'
import { persistedWorkspace } from './workspaceStore'

const storage = {
  getItem: (key: string) => Promise.resolve(window.localStorage.getItem(key)),
  setItem: (key: string, value: string) =>
    Promise.resolve(window.localStorage.setItem(key, value)),
  removeItem: (key: string) =>
    Promise.resolve(window.localStorage.removeItem(key))
}

export type DeveloperConfig = {
  language: string
  eslint: string
  git: string
  pm2: string
  manager: string
  image: string
  style: string
  skills: string
  capabilities: string[]
}
export type ProjectDraft = {
  name: string
  destinationMode: 'current' | 'custom'
  destination: string
}
type GuideState = { developer: DeveloperConfig; project: ProjectDraft }

/**
 * Transient UI state intentionally stays out of redux-persist. This gives
 * every screen a single Redux-backed source of truth without retaining form
 * inputs, credentials, or dialog state after the browser closes.
 */
type UIState = { values: Record<string, unknown> }
type UIValue = { key: string; value: unknown }
const uiSlice = createSlice({
  name: 'ui',
  initialState: { values: {} } as UIState,
  reducers: {
    initializeUIValue(state, action: PayloadAction<UIValue>) {
      if (!(action.payload.key in state.values))
        state.values[action.payload.key] = action.payload.value
    },
    setUIValue(state, action: PayloadAction<UIValue>) {
      if (Object.is(state.values[action.payload.key], action.payload.value))
        return
      state.values[action.payload.key] = action.payload.value
    },
    clearUIValue(state, action: PayloadAction<string>) {
      if (!(action.payload in state.values)) return
      delete state.values[action.payload]
    }
  }
})

const initialState: GuideState = {
  developer: {
    language: 'js',
    eslint: 'no',
    git: 'yes',
    pm2: 'yes',
    manager: 'yarn',
    image: 'none',
    style: 'css',
    skills: 'yes',
    capabilities: []
  },
  project: { name: 'alemonb', destinationMode: 'current', destination: '' }
}

const guideSlice = createSlice({
  name: 'guide',
  initialState,
  reducers: {
    setDeveloper(state, action: PayloadAction<Partial<DeveloperConfig>>) {
      Object.assign(state.developer, action.payload)
    },
    setProject(state, action: PayloadAction<Partial<ProjectDraft>>) {
      Object.assign(state.project, action.payload)
    },
    toggleCapability(state, action: PayloadAction<string>) {
      const current = state.developer.capabilities ?? []
      state.developer.capabilities = current.includes(action.payload)
        ? current.filter(item => item !== action.payload)
        : [...current, action.payload]
    }
  }
})

const persistedGuide = persistReducer(
  { key: 'alemonx-guide', storage, whitelist: ['developer', 'project'] },
  guideSlice.reducer
)
export const store = configureStore({
  reducer: {
    guide: persistedGuide,
    workspace: persistedWorkspace,
    ui: uiSlice.reducer,
    [workspaceApi.reducerPath]: workspaceApi.reducer
  },
  middleware: getDefault =>
    getDefault({ serializableCheck: false }).concat(workspaceApi.middleware)
})
export const persistor = persistStore(store)
export const { setDeveloper, setProject, toggleCapability } = guideSlice.actions
const { initializeUIValue, setUIValue, clearUIValue } = uiSlice.actions
export type RootState = ReturnType<typeof store.getState>

type StateInitializer<T> = T | (() => T)
type StoreStateSetter<T> = (value: T | ((previous: T) => T)) => void

/** Redux-backed replacement for component-local React state.
 *
 * The generated React id scopes each hook instance, so identical components
 * do not share state accidentally. Values are ephemeral by design.
 */
export function useStoreState<T>(
  initializer: StateInitializer<T>
): [T, StoreStateSetter<T>] {
  const key = useId()
  const dispatch = useDispatch()
  const initialValue = useMemo(
    () =>
      typeof initializer === 'function'
        ? (initializer as () => T)()
        : initializer,
    // Match React's one-time initializer semantics.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    []
  )
  const value = useSelector(
    (state: RootState) =>
      (state.ui.values[key] as T | undefined) ?? initialValue
  )

  useEffect(() => {
    dispatch(initializeUIValue({ key, value: initialValue }))
    return () => {
      dispatch(clearUIValue(key))
    }
  }, [dispatch, initialValue, key])

  const setValue = useCallback<StoreStateSetter<T>>(
    next => {
      const current =
        (store.getState().ui.values[key] as T | undefined) ?? initialValue
      dispatch(
        setUIValue({
          key,
          value:
            typeof next === 'function'
              ? (next as (previous: T) => T)(current)
              : next
        })
      )
    },
    [dispatch, initialValue, key]
  )

  return [value, setValue]
}
