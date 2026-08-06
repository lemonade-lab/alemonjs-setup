import type { ReleaseAsset } from './types'

export function recommendReleaseAssets(
  assets: ReleaseAsset[],
  userAgent: string
) {
  const platform = userAgent.includes('win')
    ? 'windows'
    : userAgent.includes('mac')
      ? 'macos'
      : 'linux'
  const architecture = /arm64|aarch64/.test(userAgent) ? 'arm64' : 'x64'
  const tokens = (asset: ReleaseAsset) =>
    new Set(
      asset.name
        .toLowerCase()
        .split(/[^a-z0-9_]+/)
        .filter(Boolean)
    )
  const isMetadata = (asset: ReleaseAsset) =>
    /\.sha\d*|\.sig|checksums?|latest\.yml/.test(asset.name.toLowerCase())
  const matchesSystem = (asset: ReleaseAsset) => {
    const values = tokens(asset)
    return (
      (platform === 'windows' &&
        (values.has('windows') || values.has('win32'))) ||
      (platform === 'macos' &&
        (values.has('macos') ||
          values.has('mac') ||
          values.has('darwin') ||
          values.has('osx'))) ||
      (platform === 'linux' &&
        (values.has('linux') ||
          values.has('appimage') ||
          values.has('deb') ||
          values.has('rpm')))
    )
  }
  const matchesArchitecture = (asset: ReleaseAsset) => {
    const values = tokens(asset)
    return architecture === 'arm64'
      ? values.has('arm64') || values.has('aarch64')
      : values.has('x64') || values.has('amd64') || values.has('x86_64')
  }
  const downloadable = assets.filter(asset => !isMetadata(asset))
  const hasRecommendation = downloadable.some(
    asset => matchesSystem(asset) && matchesArchitecture(asset)
  )
  const sorted = downloadable
    .slice()
    .sort(
      (left, right) =>
        Number(matchesSystem(right)) - Number(matchesSystem(left)) ||
        Number(matchesArchitecture(right)) - Number(matchesArchitecture(left))
    )

  return {
    assets: sorted,
    isRecommended: (asset: ReleaseAsset) =>
      hasRecommendation && matchesSystem(asset) && matchesArchitecture(asset)
  }
}
