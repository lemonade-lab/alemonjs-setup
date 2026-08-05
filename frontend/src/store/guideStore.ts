import { configureStore, createSlice, type PayloadAction } from '@reduxjs/toolkit'
import { persistReducer, persistStore } from 'redux-persist'
import { workspaceApi } from './workspaceApi'
import { persistedWorkspace } from './workspaceStore'

const storage = {
  getItem: (key: string) => Promise.resolve(window.localStorage.getItem(key)),
  setItem: (key: string, value: string) => Promise.resolve(window.localStorage.setItem(key, value)),
  removeItem: (key: string) => Promise.resolve(window.localStorage.removeItem(key)),
}

export type DeveloperConfig = { language: string; eslint: string; git: string; pm2: string; manager: string; image: string; style: string; skills: string; capabilities: string[] }
export type ProjectDraft = { name: string; destinationMode: 'current' | 'custom'; destination: string }
type GuideState = { developer: DeveloperConfig; project: ProjectDraft }

const initialState: GuideState = {
  developer: { language: 'js', eslint: 'no', git: 'yes', pm2: 'no', manager: 'yarn', image: 'none', style: 'css', skills: 'yes', capabilities: [] },
  project: { name: 'alemonb', destinationMode: 'current', destination: '' },
}

const guideSlice = createSlice({
  name: 'guide', initialState,
  reducers: {
    setDeveloper(state, action: PayloadAction<Partial<DeveloperConfig>>) { Object.assign(state.developer, action.payload) },
    setProject(state, action: PayloadAction<Partial<ProjectDraft>>) { Object.assign(state.project, action.payload) },
  },
})

const persistedGuide = persistReducer({ key: 'alemonjs-setup-guide', storage, whitelist: ['developer', 'project'] }, guideSlice.reducer)
export const store = configureStore({ reducer: { guide: persistedGuide, workspace: persistedWorkspace, [workspaceApi.reducerPath]: workspaceApi.reducer }, middleware: (getDefault) => getDefault({ serializableCheck: false }).concat(workspaceApi.middleware) })
export const persistor = persistStore(store)
export const { setDeveloper, setProject } = guideSlice.actions
export type RootState = ReturnType<typeof store.getState>
