import { createApi, fetchBaseQuery } from '@reduxjs/toolkit/query/react'

type RobotResult = { output: string; path?: string }
// ConsolePayload splits the terminal's live process output from its static
// project context so the terminal can render them at different refresh rates.
type ConsolePayload = {
  output: string
  snapshot: string
  running: boolean
  mode: string
  path?: string
}
type RobotTask = {
  id: string
  root: string
  action: string
  status: 'running' | 'completed' | 'failed'
  output?: string
  error?: string
}
type CatalogGroup = {
  title: string
  items: Array<{
    name: string
    description: string
    url: string
    install: string
  }>
}
type CatalogDocument = { source: string; markdown: string }
type CatalogVersions = { latest: string; versions: string[] }
type PackageConfig = {
  package: string
  namespace: string
  fields: Array<{
    name: string
    type: string
    required: boolean
    description: string
  }>
  values: Record<string, string>
}
type LocalPackages = {
  items: Array<{
    name: string
    version?: string
    description?: string
    path: string
    valid: boolean
  }>
}
type LocalPackageVersions = {
  source: 'git' | 'npm'
  current: string
  latest?: string
  versions: string[]
}
export type RuntimeOverview = {
  name: string
  version: string
  packageManager: string
  hasAppScript: boolean
  hasDevScript: boolean
  hasBuildScript: boolean
  hasStartScript: boolean
  pm2Configured: boolean
  dependenciesComplete: boolean
  platforms: Array<{
    id: string
    label: string
    package: string
    declared: boolean
    installed: boolean
    version?: string
  }>
}
export type PM2Status = {
  configured: boolean
  managed: boolean
  running: boolean
  status?: string
}
export type PM2Process = {
  id: number
  name: string
  namespace: string
  status: string
  pid: number
  memory: number
  cpu: number
  uptime: number
  restarts: number
  script: string
}
export type GitWorkspace = {
  root: string
  repository: boolean
  gitRoot?: string
  remote?: string
  branch?: string
  upstream?: string
  ahead: number
  behind: number
  changes: Array<{ status: string; path: string }>
  branches: Array<{ name: string; current: boolean; upstream?: string }>
  remoteBranches: Array<{ name: string; remote: string; branch: string }>
  commits: Array<{
    sha: string
    shortSha: string
    subject: string
    createdAt: string
  }>
  tags: Array<{ name: string; subject?: string; createdAt?: string }>
  remotes: Array<{ name: string; url: string }>
}
export type RuntimePreflight = {
  login: string
  package?: string
  missing: string[]
  summary: string[]
  dependenciesComplete: boolean
}
type PackageManifest = {
  name: string
  version: string
  description: string
  homepage: string
  repository: string
  license: string
  private: boolean
  access: string
}
export type SetupPlugin = {
  id: string
  name: string
  version: string
  description?: string
  platforms?: string[]
  navigation: { label: string; icon?: string; order?: number }
  web?: { root: string }
  runnable: boolean
  enabled: boolean
  online?: boolean
}

export const workspaceApi = createApi({
  reducerPath: 'workspaceApi',
  baseQuery: fetchBaseQuery({ baseUrl: '/api/v1/' }),
  keepUnusedDataFor: 60 * 60,
  tagTypes: [
    'RobotFile',
    'Catalog',
    'GitStatus',
    'NpmStatus',
    'PackageConfig',
    'LocalPackages',
    'PackageManifest',
    'Runtime',
    'OperationTasks',
    'SetupPlugins',
    'EnvironmentReport'
  ],
  endpoints: build => ({
    goals: build.query<unknown[], void>({ query: () => 'goals' }),
    environmentReport: build.query<
      Record<string, unknown>,
      { goalId: string; variant: string }
    >({
      query: body => ({ url: 'checks', method: 'POST', body }),
      providesTags: (_result, _error, arg) => [
        { type: 'EnvironmentReport', id: `${arg.goalId}:${arg.variant}` }
      ]
    }),
    releases: build.query<unknown[], string>({
      query: app => `releases?app=${encodeURIComponent(app)}`
    }),
    setupUpdate: build.query<
      {
        current: string
        latest?: string
        available: boolean
        releaseUrl?: string
        downloadUrl?: string
        assetName?: string
        platformMatched: boolean
        integrityReady: boolean
        downloadReady: boolean
      },
      void
    >({ query: () => 'update' }),
    setupPlugins: build.query<SetupPlugin[], void>({
      query: () => 'setup/plugins',
      providesTags: ['SetupPlugins']
    }),
    setSetupPluginEnabled: build.mutation<
      { id: string; enabled: boolean },
      { pluginID: string; enabled: boolean }
    >({
      query: ({ pluginID, ...body }) => ({
        url: `setup/plugins/${encodeURIComponent(pluginID)}/enabled`,
        method: 'POST',
        body
      }),
      invalidatesTags: ['SetupPlugins']
    }),
    installSetupPlugin: build.mutation<
      { id: string; installed: boolean },
      { pluginID: string }
    >({
      query: ({ pluginID }) => ({
        url: `setup/plugins/${encodeURIComponent(pluginID)}/install`,
        method: 'POST'
      }),
      invalidatesTags: ['SetupPlugins']
    }),
    systemMcp: build.query<{ running: boolean }, void>({
      query: () => 'system/mcp'
    }),
    startSetupPluginTask: build.mutation<
      RobotTask,
      {
        pluginID: string
        action: string
        confirm: boolean
        params?: Record<string, string>
      }
    >({
      query: ({ pluginID, ...body }) => ({
        url: `setup/plugins/${encodeURIComponent(pluginID)}/actions`,
        method: 'POST',
        body
      })
    }),
    catalog: build.query<CatalogGroup[], 'apps' | 'environment'>({
      query: kind => `catalog?kind=${kind}`,
      providesTags: (_result, _error, kind) => [{ type: 'Catalog', id: kind }]
    }),
    catalogVersions: build.query<CatalogVersions, string>({
      query: packageName =>
        `catalog/versions?${new URLSearchParams({ package: packageName })}`,
      keepUnusedDataFor: 5 * 60
    }),
    catalogDocument: build.query<CatalogDocument, string>({
      query: url => `catalog/document?${new URLSearchParams({ url })}`
    }),
    catalogPackageConfig: build.query<PackageConfig, string>({
      query: url => `catalog/package-config?${new URLSearchParams({ url })}`
    }),
    packageConfig: build.query<
      PackageConfig,
      { root: string; package: string }
    >({
      query: ({ root, package: packageName }) =>
        `robot/package-config?${new URLSearchParams({ root, package: packageName })}`,
      providesTags: (_result, _error, arg) => [
        { type: 'PackageConfig', id: `${arg.root}:${arg.package}` }
      ]
    }),
    localPackages: build.query<LocalPackages, string>({
      query: root => `robot/packages?${new URLSearchParams({ root })}`,
      providesTags: (_result, _error, root) => [
        { type: 'LocalPackages', id: root }
      ]
    }),
    localPackageVersions: build.query<
      LocalPackageVersions,
      { root: string; package: string }
    >({
      query: ({ root, package: packageName }) =>
        `robot/package-versions?${new URLSearchParams({ root, package: packageName })}`
    }),
    localPackageReadme: build.query<
      RobotResult,
      { root: string; package: string }
    >({
      query: ({ root, package: packageName }) =>
        `robot/package-readme?${new URLSearchParams({ root, package: packageName })}`
    }),
    packageManifest: build.query<PackageManifest, string>({
      query: root => `robot/manifest?${new URLSearchParams({ root })}`,
      providesTags: (_result, _error, root) => [
        { type: 'PackageManifest', id: root }
      ]
    }),
    robotTasks: build.query<RobotTask[], void>({
      query: () => 'robot/tasks',
      providesTags: ['OperationTasks']
    }),
    robotConsole: build.query<ConsolePayload, { root: string; refresh?: boolean }>({
      query: ({ root, refresh }) =>
        `robot/console?${new URLSearchParams(refresh ? { root, refresh: '1' } : { root })}`
    }),
    robotRuntime: build.query<RuntimeOverview, string>({
      query: root => `robot/runtime?${new URLSearchParams({ root })}`,
      keepUnusedDataFor: 20,
      providesTags: (_result, _error, root) => [{ type: 'Runtime', id: root }]
    }),
    robotPM2Status: build.query<PM2Status, string>({
      query: root => `robot/pm2-status?${new URLSearchParams({ root })}`,
      providesTags: (_result, _error, root) => [{ type: 'Runtime', id: root }]
    }),
    robotPM2Processes: build.query<{ items: PM2Process[] }, string>({
      query: root => `robot/pm2-processes?${new URLSearchParams({ root })}`
    }),
    appPort: build.query<{ port: number; configured: boolean }, string>({
      query: root => `robot/app-port?${new URLSearchParams({ root })}`
    }),
    robotApps: build.query<{ items: string[] }, string>({
      query: root => `robot/apps?${new URLSearchParams({ root })}`,
      providesTags: (_result, _error, root) => [
        { type: 'RobotFile', id: `${root}:alemon.config.yaml` }
      ]
    }),
    setAppEnabled: build.mutation<
      RobotResult,
      { root: string; package: string; enabled: boolean }
    >({
      query: ({ root, package: packageName, enabled }) => ({
        url: `robot/apps?${new URLSearchParams({ root })}`,
        method: 'POST',
        body: { package: packageName, enabled }
      }),
      invalidatesTags: (_result, _error, body) => [
        { type: 'RobotFile', id: `${body.root}:alemon.config.yaml` }
      ]
    }),
    saveAppPort: build.mutation<
      RobotResult,
      { root: string; port: number }
    >({
      query: ({ root, port }) => ({
        url: `robot/app-port?${new URLSearchParams({ root })}`,
        method: 'POST',
        body: { port }
      }),
      invalidatesTags: (_result, _error, body) => [
        { type: 'RobotFile', id: `${body.root}:alemon.config.yaml` },
        { type: 'Runtime', id: body.root }
      ]
    }),
    robotRuntimePreflight: build.query<RuntimePreflight, string>({
      query: root => `robot/runtime/preflight?${new URLSearchParams({ root })}`
    }),
    robotProject: build.query<
      { valid: boolean; path?: string; error?: string },
      string
    >({
      query: root => `robot/validate?${new URLSearchParams({ root })}`,
      keepUnusedDataFor: 60
    }),
    robotFile: build.query<RobotResult, { root: string; file: string }>({
      query: ({ root, file }) => `robot?${new URLSearchParams({ root, file })}`,
      providesTags: (_result, _error, arg) => [
        { type: 'RobotFile', id: `${arg.root}:${arg.file}` }
      ]
    }),
    gitStatus: build.query<Record<string, unknown>, string>({
      query: root => `publish/git/status?${new URLSearchParams({ root })}`,
      providesTags: (_result, _error, root) => [{ type: 'GitStatus', id: root }]
    }),
    gitWorkspace: build.query<
      GitWorkspace,
      { root: string; view: 'commit' | 'history' | 'tag' | 'branch' | 'remote' }
    >({
      query: ({ root, view }) =>
        `robot/git?${new URLSearchParams({ root, view })}`,
      providesTags: (_result, _error, arg) => [
        { type: 'GitStatus', id: arg.root }
      ]
    }),
    npmStatus: build.query<Record<string, unknown>, string>({
      query: root => `publish/npm/status?${new URLSearchParams({ root })}`,
      providesTags: (_result, _error, root) => [{ type: 'NpmStatus', id: root }]
    }),
    npmPack: build.query<
      Record<string, unknown>,
      { root: string; commit?: string }
    >({
      query: ({ root, commit }) =>
        `publish/npm/pack?${new URLSearchParams(commit ? { root, commit } : { root })}`
    }),
    robotOperation: build.mutation<RobotResult, Record<string, string>>({
      query: body => ({ url: 'robot', method: 'POST', body }),
      invalidatesTags: (_result, _error, body) => [
        { type: 'GitStatus', id: body.root },
        { type: 'NpmStatus', id: body.root },
        { type: 'LocalPackages', id: body.root }
      ]
    }),
    gitWorkspaceAction: build.mutation<
      RobotResult,
      {
        root: string
        action:
          | 'fetch'
          | 'pull'
          | 'push'
          | 'commit'
          | 'branch-create'
          | 'branch-switch'
          | 'branch-track'
          | 'branch-delete'
          | 'tag-create'
          | 'tag-push'
          | 'tag-delete'
          | 'remote-add'
          | 'remote-set-url'
          | 'remote-remove'
        value?: string
        message?: string
      }
    >({
      query: body => ({ url: 'robot/git', method: 'POST', body }),
      invalidatesTags: (_result, _error, body) => [
        { type: 'GitStatus', id: body.root }
      ]
    }),
    startRobotTask: build.mutation<RobotTask, Record<string, string>>({
      query: body => ({ url: 'robot/tasks', method: 'POST', body }),
      invalidatesTags: (_result, _error, body) => [
        { type: 'GitStatus', id: body.root },
        { type: 'NpmStatus', id: body.root },
        { type: 'Runtime', id: body.root },
        'OperationTasks'
      ]
    }),
    writeRobotFile: build.mutation<
      RobotResult,
      { root: string; file: string; content: string }
    >({
      query: body => ({ url: 'robot', method: 'PUT', body }),
      invalidatesTags: (_result, _error, body) => {
        const tags: (
          | { type: 'RobotFile'; id: string }
          | { type: 'PackageConfig'; id?: string }
        )[] = [{ type: 'RobotFile', id: `${body.root}:${body.file}` }]
        // Saving alemon.config.yaml changes every structured config view
        // (current project config, connection package config, start dialog),
        // so drop all cached PackageConfig entries along with the file tag.
        if (body.file === 'alemon.config.yaml')
          tags.push({ type: 'PackageConfig' })
        return tags
      }
    }),
    writePackageManifest: build.mutation<
      RobotResult,
      { root: string } & PackageManifest
    >({
      query: body => ({ url: 'robot/manifest', method: 'PUT', body }),
      invalidatesTags: (_result, _error, body) => [
        { type: 'PackageManifest', id: body.root },
        { type: 'GitStatus', id: body.root },
        { type: 'NpmStatus', id: body.root }
      ]
    }),
    writePackageConfig: build.mutation<
      RobotResult,
      { root: string; package: string; values: Record<string, string> }
    >({
      query: body => ({ url: 'robot/package-config', method: 'PUT', body }),
      invalidatesTags: (_result, _error, body) => [
        // The whole file changed, so every structured view (current project,
        // connection package, start dialog) must re-parse it.
        { type: 'PackageConfig' },
        { type: 'RobotFile', id: `${body.root}:alemon.config.yaml` }
      ]
    }),
    saveRobotLogin: build.mutation<
      RobotResult,
      { root: string; login: string; package?: string }
    >({
      query: body => ({ url: 'robot/login', method: 'POST', body }),
      invalidatesTags: (_result, _error, body) => [
        { type: 'RobotFile', id: `${body.root}:alemon.config.yaml` },
        { type: 'PackageConfig' }
      ]
    }),
    initializeGit: build.mutation<
      RobotResult,
      {
        root: string
        authorName: string
        authorEmail: string
        repository: string
        message: string
      }
    >({
      query: body => ({ url: 'robot/git-init', method: 'POST', body }),
      invalidatesTags: (_result, _error, body) => [
        { type: 'GitStatus', id: body.root }
      ]
    })
  })
})

export const {
  useGoalsQuery,
  useLazyEnvironmentReportQuery,
  useReleasesQuery,
  useLazySetupUpdateQuery,
  useSetupPluginsQuery,
  useSetSetupPluginEnabledMutation,
  useInstallSetupPluginMutation,
  useSystemMcpQuery,
  useStartSetupPluginTaskMutation,
  useCatalogQuery,
  useCatalogVersionsQuery,
  useCatalogDocumentQuery,
  useCatalogPackageConfigQuery,
  usePackageConfigQuery,
  useLazyPackageConfigQuery,
  useLocalPackagesQuery,
  useLocalPackageVersionsQuery,
  useLocalPackageReadmeQuery,
  usePackageManifestQuery,
  useRobotTasksQuery,
  useLazyRobotConsoleQuery,
  useRobotRuntimeQuery,
  useRobotPM2StatusQuery,
  useRobotPM2ProcessesQuery,
  useAppPortQuery,
  useLazyAppPortQuery,
  useSaveAppPortMutation,
  useRobotAppsQuery,
  useSetAppEnabledMutation,
  useLazyRobotRuntimePreflightQuery,
  useLazyRobotProjectQuery,
  useLazyRobotFileQuery,
  useGitStatusQuery,
  useGitWorkspaceQuery,
  useGitWorkspaceActionMutation,
  useNpmStatusQuery,
  useLazyNpmPackQuery,
  useRobotOperationMutation,
  useStartRobotTaskMutation,
  useWriteRobotFileMutation,
  useWritePackageManifestMutation,
  useWritePackageConfigMutation,
  useSaveRobotLoginMutation,
  useInitializeGitMutation
} = workspaceApi
