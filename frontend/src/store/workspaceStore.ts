import { createSlice, type PayloadAction } from '@reduxjs/toolkit'
import { createTransform, persistReducer } from 'redux-persist'

const storage = {
  getItem: (key: string) => Promise.resolve(window.localStorage.getItem(key)),
  setItem: (key: string, value: string) => Promise.resolve(window.localStorage.setItem(key, value)),
  removeItem: (key: string) => Promise.resolve(window.localStorage.removeItem(key)),
}

const workspacePersistKey = 'persist:alemonjs-setup-workspace'
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

export type WorkspaceProject = { id: string; path: string; name: string }
type WorkspaceState = { projects: WorkspaceProject[]; activeProjectID: string; drafts: Record<string, string>; developerMode: boolean }

const initialState: WorkspaceState = { projects: [], activeProjectID: '', drafts: {}, developerMode: false }
const workspaceSlice = createSlice({
  name: 'workspace', initialState,
  reducers: {
    addProjects(state, action: PayloadAction<WorkspaceProject[]>) { const known = new Set(state.projects.map((item) => item.path)); const additions = action.payload.filter((item) => !known.has(item.path)); state.projects.push(...additions); if (additions[0]) state.activeProjectID = additions[0].id },
    selectProject(state, action: PayloadAction<string>) { state.activeProjectID = action.payload },
    removeProject(state, action: PayloadAction<string>) { state.projects = state.projects.filter((item) => item.id !== action.payload); if (state.activeProjectID === action.payload) state.activeProjectID = state.projects[0]?.id ?? '' },
    setDraft(state, action: PayloadAction<{ key: string; content: string }>) { state.drafts[action.payload.key] = action.payload.content },
    setDeveloperMode(state, action: PayloadAction<boolean>) { state.developerMode = action.payload },
    clearDraft(state, action: PayloadAction<string>) { delete state.drafts[action.payload] },
  },
})

// Environment, npm registry and robot configuration files can contain tokens,
// passwords and connection credentials. Keep an unsaved edit while the page
// is open, but never write those values to browser localStorage.
const privateDraftsTransform = createTransform<Record<string, string>, Record<string, string>>(
  (drafts) => Object.fromEntries(Object.entries(drafts).filter(([key]) => !isPrivateDraft(key))),
  (drafts) => Object.fromEntries(Object.entries(drafts).filter(([key]) => !isPrivateDraft(key))),
  { whitelist: ['drafts'] }
)

export const persistedWorkspace = persistReducer({
  key: 'alemonjs-setup-workspace',
  storage,
  whitelist: ['projects', 'activeProjectID', 'drafts', 'developerMode'],
  transforms: [privateDraftsTransform]
}, workspaceSlice.reducer)
export const { addProjects, selectProject, removeProject, setDraft, clearDraft, setDeveloperMode } = workspaceSlice.actions
