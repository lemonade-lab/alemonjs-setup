import { createApi, fetchBaseQuery } from '@reduxjs/toolkit/query/react'

type RobotResult = { output: string; path?: string }
type CatalogGroup = { title: string; items: Array<{ name: string; description: string; url: string; install: string }> }
type CatalogDocument = { source: string; markdown: string }
type PackageConfig = { package: string; namespace: string; fields: Array<{ name: string; type: string; required: boolean; description: string }>; values: Record<string, string> }
type LocalPackages = { items: Array<{ name: string; path: string }> }

export const workspaceApi = createApi({
  reducerPath: 'workspaceApi',
  baseQuery: fetchBaseQuery({ baseUrl: '/api/v1/' }),
  keepUnusedDataFor: 60 * 60,
  tagTypes: ['RobotFile', 'Catalog', 'GitStatus', 'NpmStatus', 'PackageConfig'],
  endpoints: (build) => ({
    goals: build.query<unknown[], void>({ query: () => 'goals' }),
    environmentReport: build.query<Record<string, unknown>, { goalId: string; variant: string }>({ query: (body) => ({ url: 'checks', method: 'POST', body }), keepUnusedDataFor: 5 * 60 }),
    releases: build.query<unknown[], string>({ query: (app) => `releases?app=${encodeURIComponent(app)}` }),
    catalog: build.query<CatalogGroup[], 'apps' | 'environment'>({ query: (kind) => `catalog?kind=${kind}`, providesTags: (_result, _error, kind) => [{ type: 'Catalog', id: kind }] }),
    catalogDocument: build.query<CatalogDocument, string>({ query: (url) => `catalog/document?${new URLSearchParams({ url })}` }),
    catalogPackageConfig: build.query<PackageConfig, string>({ query: (url) => `catalog/package-config?${new URLSearchParams({ url })}` }),
    packageConfig: build.query<PackageConfig, { root: string; package: string }>({ query: ({ root, package: packageName }) => `robot/package-config?${new URLSearchParams({ root, package: packageName })}`, providesTags: (_result, _error, arg) => [{ type: 'PackageConfig', id: `${arg.root}:${arg.package}` }] }),
    localPackages: build.query<LocalPackages, string>({ query: (root) => `robot/packages?${new URLSearchParams({ root })}` }),
    robotFile: build.query<RobotResult, { root: string; file: string }>({ query: ({ root, file }) => `robot?${new URLSearchParams({ root, file })}`, providesTags: (_result, _error, arg) => [{ type: 'RobotFile', id: `${arg.root}:${arg.file}` }] }),
    gitStatus: build.query<Record<string, unknown>, string>({ query: (root) => `publish/git/status?${new URLSearchParams({ root })}`, providesTags: (_result, _error, root) => [{ type: 'GitStatus', id: root }] }),
    npmStatus: build.query<Record<string, unknown>, string>({ query: (root) => `publish/npm/status?${new URLSearchParams({ root })}`, providesTags: (_result, _error, root) => [{ type: 'NpmStatus', id: root }] }),
    npmPack: build.query<Record<string, unknown>, string>({ query: (root) => `publish/npm/pack?${new URLSearchParams({ root })}` }),
    robotOperation: build.mutation<RobotResult, Record<string, string>>({
      query: (body) => ({ url: 'robot', method: 'POST', body }),
      invalidatesTags: (_result, _error, body) => [{ type: 'GitStatus', id: body.root }, { type: 'NpmStatus', id: body.root }],
    }),
    writeRobotFile: build.mutation<RobotResult, { root: string; file: string; content: string }>({
      query: (body) => ({ url: 'robot', method: 'PUT', body }),
      invalidatesTags: (_result, _error, body) => [{ type: 'RobotFile', id: `${body.root}:${body.file}` }],
    }),
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

export const { useGoalsQuery, useLazyEnvironmentReportQuery, useReleasesQuery, useCatalogQuery, useCatalogDocumentQuery, useCatalogPackageConfigQuery, usePackageConfigQuery, useLocalPackagesQuery, useLazyRobotFileQuery, useGitStatusQuery, useNpmStatusQuery, useLazyNpmPackQuery, useRobotOperationMutation, useWriteRobotFileMutation, useWritePackageConfigMutation, useInitializeGitMutation } = workspaceApi
