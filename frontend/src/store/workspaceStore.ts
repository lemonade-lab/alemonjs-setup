import { createSlice, type PayloadAction } from '@reduxjs/toolkit'
import { persistReducer } from 'redux-persist'

const storage = {
  getItem: (key: string) => Promise.resolve(window.localStorage.getItem(key)),
  setItem: (key: string, value: string) => Promise.resolve(window.localStorage.setItem(key, value)),
  removeItem: (key: string) => Promise.resolve(window.localStorage.removeItem(key)),
}

export type WorkspaceProject = { id: string; path: string; name: string }
type WorkspaceState = { projects: WorkspaceProject[]; activeProjectID: string; drafts: Record<string, string> }

const initialState: WorkspaceState = { projects: [], activeProjectID: '', drafts: {} }
const workspaceSlice = createSlice({
  name: 'workspace', initialState,
  reducers: {
    addProjects(state, action: PayloadAction<WorkspaceProject[]>) { const known = new Set(state.projects.map((item) => item.path)); const additions = action.payload.filter((item) => !known.has(item.path)); state.projects.push(...additions); if (additions[0]) state.activeProjectID = additions[0].id },
    selectProject(state, action: PayloadAction<string>) { state.activeProjectID = action.payload },
    removeProject(state, action: PayloadAction<string>) { state.projects = state.projects.filter((item) => item.id !== action.payload); if (state.activeProjectID === action.payload) state.activeProjectID = state.projects[0]?.id ?? '' },
    setDraft(state, action: PayloadAction<{ key: string; content: string }>) { state.drafts[action.payload.key] = action.payload.content },
    clearDraft(state, action: PayloadAction<string>) { delete state.drafts[action.payload] },
  },
})

export const persistedWorkspace = persistReducer({ key: 'alemonjs-setup-workspace', storage, whitelist: ['projects', 'activeProjectID', 'drafts'] }, workspaceSlice.reducer)
export const { addProjects, selectProject, removeProject, setDraft, clearDraft } = workspaceSlice.actions
