import { createApi, fetchBaseQuery } from '@reduxjs/toolkit/query/react'

type RobotResult = { output: string; path?: string }
type RobotTask = { id: string; root: string; action: string; status: 'running' | 'completed' | 'failed'; output?: string; error?: string }
type CatalogGroup = { title: string; items: Array<{ name: string; description: string; url: string; install: string }> }
type CatalogDocument = { source: string; markdown: string }
type CatalogVersions = { latest: string; versions: string[] }
type PackageConfig = { package: string; namespace: string; fields: Array<{ name: string; type: string; required: boolean; description: string }>; values: Record<string, string> }
type LocalPackages = { items: Array<{ name: string; version?: string; description?: string; path: string; valid: boolean }> }
type PackageManifest = { name: string; version: string; description: string; homepage: string; repository: string; license: string; private: boolean; access: string }
export type SetupPlugin = { id: string; name: string; version: string; description?: string; platforms?: string[]; navigation: { label: string; icon?: string; order?: number }; pages: Array<{ id: string; label: string; description?: string }>; actions?: Array<{ id: string; label: string; description?: string; confirm?: boolean; page?: string; fields?: Array<{ key: string; label: string; type: 'select' | 'number' | 'text'; default?: string; options?: Array<{ label: string; value: string }> }> }>; runnable: boolean; enabled: boolean }

export const workspaceApi = createApi({
  reducerPath: 'workspaceApi',
  baseQuery: fetchBaseQuery({ baseUrl: '/api/v1/' }),
  keepUnusedDataFor: 60 * 60,
  tagTypes: ['RobotFile', 'Catalog', 'GitStatus', 'NpmStatus', 'PackageConfig', 'LocalPackages', 'PackageManifest', 'SetupPlugins'],
  endpoints: (build) => ({
    goals: build.query<unknown[], void>({ query: () => 'goals' }),
    environmentReport: build.query<Record<string, unknown>, { goalId: string; variant: string }>({ query: (body) => ({ url: 'checks', method: 'POST', body }), keepUnusedDataFor: 5 * 60 }),
    releases: build.query<unknown[], string>({ query: (app) => `releases?app=${encodeURIComponent(app)}` }),
    setupUpdate: build.query<{ current: string; latest?: string; available: boolean; releaseUrl?: string; downloadUrl?: string; assetName?: string; platformMatched: boolean }, void>({ query: () => 'update' }),
    setupPlugins: build.query<SetupPlugin[], void>({ query: () => 'setup/plugins', providesTags: ['SetupPlugins'] }),
    setSetupPluginEnabled: build.mutation<{ id: string; enabled: boolean }, { pluginID: string; enabled: boolean }>({ query: ({ pluginID, ...body }) => ({ url: `setup/plugins/${encodeURIComponent(pluginID)}/enabled`, method: 'POST', body }), invalidatesTags: ['SetupPlugins'] }),
    startSetupPluginTask: build.mutation<RobotTask, { pluginID: string; action: string; confirm: boolean; params?: Record<string, string> }>({ query: ({ pluginID, ...body }) => ({ url: `setup/plugins/${encodeURIComponent(pluginID)}/actions`, method: 'POST', body }) }),
    catalog: build.query<CatalogGroup[], 'apps' | 'environment'>({ query: (kind) => `catalog?kind=${kind}`, providesTags: (_result, _error, kind) => [{ type: 'Catalog', id: kind }] }),
    catalogVersions: build.query<CatalogVersions, string>({ query: (packageName) => `catalog/versions?${new URLSearchParams({ package: packageName })}`, keepUnusedDataFor: 5 * 60 }),
    catalogDocument: build.query<CatalogDocument, string>({ query: (url) => `catalog/document?${new URLSearchParams({ url })}` }),
    catalogPackageConfig: build.query<PackageConfig, string>({ query: (url) => `catalog/package-config?${new URLSearchParams({ url })}` }),
    packageConfig: build.query<PackageConfig, { root: string; package: string }>({ query: ({ root, package: packageName }) => `robot/package-config?${new URLSearchParams({ root, package: packageName })}`, providesTags: (_result, _error, arg) => [{ type: 'PackageConfig', id: `${arg.root}:${arg.package}` }] }),
    localPackages: build.query<LocalPackages, string>({ query: (root) => `robot/packages?${new URLSearchParams({ root })}`, providesTags: (_result, _error, root) => [{ type: 'LocalPackages', id: root }] }),
    packageManifest: build.query<PackageManifest, string>({ query: (root) => `robot/manifest?${new URLSearchParams({ root })}`, providesTags: (_result, _error, root) => [{ type: 'PackageManifest', id: root }] }),
    robotTasks: build.query<RobotTask[], void>({ query: () => 'robot/tasks' }),
    robotConsole: build.query<RobotResult, string>({ query: (root) => `robot/console?${new URLSearchParams({ root })}` }),
    robotProject: build.query<{ valid: boolean; path?: string; error?: string }, string>({ query: (root) => `robot/validate?${new URLSearchParams({ root })}`, keepUnusedDataFor: 60 }),
    robotFile: build.query<RobotResult, { root: string; file: string }>({ query: ({ root, file }) => `robot?${new URLSearchParams({ root, file })}`, providesTags: (_result, _error, arg) => [{ type: 'RobotFile', id: `${arg.root}:${arg.file}` }] }),
    gitStatus: build.query<Record<string, unknown>, string>({ query: (root) => `publish/git/status?${new URLSearchParams({ root })}`, providesTags: (_result, _error, root) => [{ type: 'GitStatus', id: root }] }),
    npmStatus: build.query<Record<string, unknown>, string>({ query: (root) => `publish/npm/status?${new URLSearchParams({ root })}`, providesTags: (_result, _error, root) => [{ type: 'NpmStatus', id: root }] }),
    npmPack: build.query<Record<string, unknown>, { root: string; commit?: string }>({ query: ({ root, commit }) => `publish/npm/pack?${new URLSearchParams(commit ? { root, commit } : { root })}` }),
    robotOperation: build.mutation<RobotResult, Record<string, string>>({
      query: (body) => ({ url: 'robot', method: 'POST', body }),
      invalidatesTags: (_result, _error, body) => [{ type: 'GitStatus', id: body.root }, { type: 'NpmStatus', id: body.root }, { type: 'LocalPackages', id: body.root }],
    }),
    startRobotTask: build.mutation<RobotTask, Record<string, string>>({
      query: (body) => ({ url: 'robot/tasks', method: 'POST', body }),
      invalidatesTags: (_result, _error, body) => [{ type: 'GitStatus', id: body.root }, { type: 'NpmStatus', id: body.root }],
    }),
    writeRobotFile: build.mutation<RobotResult, { root: string; file: string; content: string }>({
      query: (body) => ({ url: 'robot', method: 'PUT', body }),
      invalidatesTags: (_result, _error, body) => [{ type: 'RobotFile', id: `${body.root}:${body.file}` }],
    }),
    writePackageManifest: build.mutation<RobotResult, { root: string } & PackageManifest>({ query: (body) => ({ url: 'robot/manifest', method: 'PUT', body }), invalidatesTags: (_result, _error, body) => [{ type: 'PackageManifest', id: body.root }, { type: 'GitStatus', id: body.root }, { type: 'NpmStatus', id: body.root }] }),
    writePackageConfig: build.mutation<RobotResult, { root: string; package: string; values: Record<string, string> }>({
      query: (body) => ({ url: 'robot/package-config', method: 'PUT', body }),
      invalidatesTags: (_result, _error, body) => [{ type: 'PackageConfig', id: `${body.root}:${body.package}` }, { type: 'RobotFile', id: `${body.root}:alemon.config.yaml` }],
    }),
    initializeGit: build.mutation<RobotResult, { root: string; authorName: string; authorEmail: string; repository: string; message: string }>({
      query: (body) => ({ url: 'robot/git-init', method: 'POST', body }),
      invalidatesTags: (_result, _error, body) => [{ type: 'GitStatus', id: body.root }],
    }),
  }),
})

export const { useGoalsQuery, useLazyEnvironmentReportQuery, useReleasesQuery, useLazySetupUpdateQuery, useSetupPluginsQuery, useSetSetupPluginEnabledMutation, useStartSetupPluginTaskMutation, useCatalogQuery, useCatalogVersionsQuery, useCatalogDocumentQuery, useCatalogPackageConfigQuery, usePackageConfigQuery, useLocalPackagesQuery, usePackageManifestQuery, useRobotTasksQuery, useLazyRobotConsoleQuery, useLazyRobotProjectQuery, useLazyRobotFileQuery, useGitStatusQuery, useNpmStatusQuery, useLazyNpmPackQuery, useRobotOperationMutation, useStartRobotTaskMutation, useWriteRobotFileMutation, useWritePackageManifestMutation, useWritePackageConfigMutation, useInitializeGitMutation } = workspaceApi
