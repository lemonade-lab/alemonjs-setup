import { createSlice, type PayloadAction } from '@reduxjs/toolkit'
import { createTransform, persistReducer } from 'redux-persist'

const storage = {
  getItem: (key: string) => Promise.resolve(window.localStorage.getItem(key)),
  setItem: (key: string, value: string) =>
    Promise.resolve(window.localStorage.setItem(key, value)),
  removeItem: (key: string) =>
    Promise.resolve(window.localStorage.removeItem(key))
}

const workspacePersistKey = 'persist:alemonx-workspace'
const isPrivateDraft = (key: string) =>
  key.endsWith(':.env') ||
  key.endsWith(':.npmrc') ||
  key.endsWith(':alemon.config.yaml') ||
  key.endsWith(':alemon.config.yml')

// Remove values stored by older releases as soon as this module is loaded.
// A redux-persist transform protects future writes, but would otherwise leave
// an old raw value in localStorage until the next state change.
function removeLegacyPrivateDrafts() {
  try {
    const stored = window.localStorage.getItem(workspacePersistKey)
    if (!stored) return
    const state = JSON.parse(stored) as Record<string, string>
    const drafts = JSON.parse(state.drafts ?? '{}') as Record<string, string>
    const cleanDrafts = Object.fromEntries(
      Object.entries(drafts).filter(([key]) => !isPrivateDraft(key))
    )
    if (Object.keys(cleanDrafts).length === Object.keys(drafts).length) return
    state.drafts = JSON.stringify(cleanDrafts)
    window.localStorage.setItem(workspacePersistKey, JSON.stringify(state))
  } catch {
    // Persisted workspace data is optional; ignore malformed legacy entries.
  }
}

removeLegacyPrivateDrafts()

export type WorkspaceProject = {
  id: string
  path: string
  name: string
  pinned?: boolean
}
type WorkspaceState = {
  projects: WorkspaceProject[]
  activeProjectID: string
  drafts: Record<string, string>
  developerMode: boolean
}

const initialState: WorkspaceState = {
  projects: [],
  activeProjectID: '',
  drafts: {},
  developerMode: false
}
const workspaceSlice = createSlice({
  name: 'workspace',
  initialState,
  reducers: {
    addProjects(state, action: PayloadAction<WorkspaceProject[]>) {
      const known = new Set(state.projects.map(item => item.path))
      const additions = action.payload.filter(item => !known.has(item.path))
      state.projects.push(...additions)
      if (additions[0]) state.activeProjectID = additions[0].id
    },
    selectProject(state, action: PayloadAction<string>) {
      state.activeProjectID = action.payload
    },
    removeProject(state, action: PayloadAction<string>) {
      state.projects = state.projects.filter(item => item.id !== action.payload)
      if (state.activeProjectID === action.payload)
        state.activeProjectID = state.projects[0]?.id ?? ''
    },
    pinProject(state, action: PayloadAction<string>) {
      const target = state.projects.find(item => item.id === action.payload)
      if (!target) return
      target.pinned = !target.pinned
      state.projects = [...state.projects].sort((left, right) => {
        if (left.pinned && !right.pinned) return -1
        if (!left.pinned && right.pinned) return 1
        return 0
      })
    },
    setDraft(state, action: PayloadAction<{ key: string; content: string }>) {
      state.drafts[action.payload.key] = action.payload.content
    },
    setDeveloperMode(state, action: PayloadAction<boolean>) {
      state.developerMode = action.payload
    },
    clearDraft(state, action: PayloadAction<string>) {
      delete state.drafts[action.payload]
    }
  }
})

// Environment, npm registry and robot configuration files can contain tokens,
// passwords and connection credentials. Keep an unsaved edit while the page
// is open, but never write those values to browser localStorage.
const privateDraftsTransform = createTransform<
  Record<string, string>,
  Record<string, string>
>(
  drafts =>
    Object.fromEntries(
      Object.entries(drafts).filter(([key]) => !isPrivateDraft(key))
    ),
  drafts =>
    Object.fromEntries(
      Object.entries(drafts).filter(([key]) => !isPrivateDraft(key))
    ),
  { whitelist: ['drafts'] }
)

export const persistedWorkspace = persistReducer(
  {
    key: 'alemonx-workspace',
    storage,
    whitelist: [
      'projects',
      'activeProjectID',
      'drafts',
      'developerMode'
    ],
    transforms: [privateDraftsTransform],
    // A stored state written by an older release may hold null/undefined for
    // fields this build expects to be arrays. Never let that leak into the
    // live state, or components calling .find on them crash on first render.
    stateReconciler: (inbound, _original, reduced) => {
      const restored = { ...reduced, ...(inbound ?? {}) } as WorkspaceState
      if (!Array.isArray(restored.projects)) restored.projects = []
      if (!restored.activeProjectID) restored.activeProjectID = ''
      return restored
    }
  },
  workspaceSlice.reducer
)
export const {
  addProjects,
  selectProject,
  removeProject,
  pinProject,
  setDraft,
  clearDraft,
  setDeveloperMode
} = workspaceSlice.actions
