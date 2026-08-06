export type Mirror = { name: string; url: string }

export type Goal = {
  id: string
  title: string
  description: string
  steps: string[]
  downloadUrl?: string
  mirrors?: Mirror[]
}

export type Check = {
  id: string
  name: string
  status: 'ready' | 'missing' | 'warning'
  detail: string
  suggestion: string
}

export type Report = {
  ready: boolean
  platform: string
  checks: Check[]
  checkedAt: string
}

export type ProjectConfig = {
  template: 'bot' | 'dev'
  name: string
  destinationMode: 'current' | 'custom'
  destination: string
  language: string
  packageManager: string
  eslint: boolean
  initializeGit: boolean
  usePM2: boolean
  imageMode: string
  styleMode: string
  downloadSkills: boolean
  developmentPackages: string[]
}

export type Creation = { path?: string; status?: string; logs?: string[] }
export type ReleaseAsset = { name: string; url: string; size: number }
export type Release = {
  tag: string
  name: string
  url: string
  publishedAt: string
  assets: ReleaseAsset[]
}
