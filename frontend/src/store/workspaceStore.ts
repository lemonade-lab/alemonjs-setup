import { createSlice, type PayloadAction } from '@reduxjs/toolkit'
import { createTransform, persistReducer } from 'redux-persist'

const storage = {
  getItem: (key: string) => Promise.resolve(window.localStorage.getItem(key)),
  setItem: (key: string, value: string) =>
    Promise.resolve(window.localStorage.setItem(key, value)),
  removeItem: (key: string) =>
    Promise.resolve(window.localStorage.removeItem(key))
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
export type WorkspaceWebViewTab = {
  key: string
  root: string
  entryID: string
  package: string
  title: string
  openedAt: string
  lastActiveAt: string
}
type WorkspaceState = {
  projects: WorkspaceProject[]
  activeProjectID: string
  drafts: Record<string, string>
  developerMode: boolean
  webviewTabs: WorkspaceWebViewTab[]
  activeWebviewTabKey: string
}

const initialState: WorkspaceState = {
  projects: [],
  activeProjectID: '',
  drafts: {},
  developerMode: false,
  webviewTabs: [],
  activeWebviewTabKey: ''
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
      state.activeWebviewTabKey = ''
      const root = state.projects.find(item => item.id === action.payload)?.path
      if (!root) return
      const recent = state.webviewTabs
        .filter(item => item.root === root)
        .sort((left, right) =>
          right.lastActiveAt.localeCompare(left.lastActiveAt)
        )[0]
      state.activeWebviewTabKey = recent?.key ?? ''
    },
    removeProject(state, action: PayloadAction<string>) {
      state.projects = state.projects.filter(item => item.id !== action.payload)
      if (state.activeProjectID === action.payload)
        state.activeProjectID = state.projects[0]?.id ?? ''
    },
    setDraft(state, action: PayloadAction<{ key: string; content: string }>) {
      state.drafts[action.payload.key] = action.payload.content
    },
    setDeveloperMode(state, action: PayloadAction<boolean>) {
      state.developerMode = action.payload
    },
    openWebviewTab(
      state,
      action: PayloadAction<
        Omit<WorkspaceWebViewTab, 'openedAt' | 'lastActiveAt'>
      >
    ) {
      const now = new Date().toISOString()
      const existing = state.webviewTabs.find(
        item => item.key === action.payload.key
      )
      if (existing) existing.lastActiveAt = now
      else
        state.webviewTabs.push({
          ...action.payload,
          openedAt: now,
          lastActiveAt: now
        })
      state.activeWebviewTabKey = action.payload.key
    },
    activateWebviewTab(state, action: PayloadAction<string>) {
      const tab = state.webviewTabs.find(item => item.key === action.payload)
      if (!tab) return
      tab.lastActiveAt = new Date().toISOString()
      state.activeWebviewTabKey = tab.key
    },
    clearActiveWebviewTab(state) {
      state.activeWebviewTabKey = ''
    },
    closeWebviewTab(state, action: PayloadAction<string>) {
      const index = state.webviewTabs.findIndex(
        item => item.key === action.payload
      )
      if (index < 0) return
      const active = state.activeWebviewTabKey === action.payload
      state.webviewTabs.splice(index, 1)
      if (active)
        state.activeWebviewTabKey =
          state.webviewTabs[index]?.key ??
          state.webviewTabs[index - 1]?.key ??
          ''
    },
    pruneWebviewTabs(
      state,
      action: PayloadAction<{ root: string; entryIDs: string[] }>
    ) {
      state.webviewTabs = state.webviewTabs.filter(
        item =>
          item.root !== action.payload.root ||
          action.payload.entryIDs.includes(item.entryID)
      )
      if (
        !state.webviewTabs.some(item => item.key === state.activeWebviewTabKey)
      )
        state.activeWebviewTabKey = ''
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
    key: 'alemonjs-setup-workspace',
    storage,
    whitelist: [
      'projects',
      'activeProjectID',
      'drafts',
      'developerMode',
      'webviewTabs',
      'activeWebviewTabKey'
    ],
    transforms: [privateDraftsTransform]
  },
  workspaceSlice.reducer
)
export const {
  addProjects,
  selectProject,
  removeProject,
  setDraft,
  clearDraft,
  setDeveloperMode,
  openWebviewTab,
  activateWebviewTab,
  clearActiveWebviewTab,
  closeWebviewTab,
  pruneWebviewTabs
} = workspaceSlice.actions
