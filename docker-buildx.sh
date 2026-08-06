#!/usr/bin/env sh
set -eu

# 国内线上默认推送腾讯云 CCR；离线服务器可改 TARGET=archive 生成导入包。
REGISTRY=${REGISTRY:-ccr.ccs.tencentyun.com/ningmengchongshui}
IMAGE_NAME=${IMAGE_NAME:-alemonx}
VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}
PLATFORMS=${PLATFORMS:-linux/amd64,linux/arm64,linux/arm/v7}
BUILDER=${BUILDER:-alx-builder}
TARGET=${TARGET:-tencent}
IMAGE="${REGISTRY}/${IMAGE_NAME}"

if ! docker buildx inspect "$BUILDER" >/dev/null 2>&1; then
  docker buildx create --name "$BUILDER" --use --config ./buildkitd.toml
else
  docker buildx use "$BUILDER"
fi
docker buildx inspect --bootstrap >/dev/null

case "$TARGET" in
  tencent)
    echo "发布 ${IMAGE}:${VERSION} 到腾讯云 CCR（${PLATFORMS}）"
    docker buildx build --platform "$PLATFORMS" --build-arg "VERSION=$VERSION" --tag "${IMAGE}:latest" --tag "${IMAGE}:${VERSION}" --push .
    ;;
  archive)
    PLATFORM=${PLATFORM:-linux/amd64}
    ARCHIVE=${ARCHIVE:-dist/alx-${VERSION}-${PLATFORM#linux/}.oci.tar}
    mkdir -p "$(dirname "$ARCHIVE")"
    echo "构建离线镜像包 ${IMAGE}:${VERSION}（${PLATFORM}）"
    docker buildx build --platform "$PLATFORM" --build-arg "VERSION=$VERSION" --tag "${IMAGE}:${VERSION}" --output "type=oci,dest=${ARCHIVE},name=${IMAGE}:${VERSION}" .
    echo "离线镜像已生成：${ARCHIVE}"
    ;;
  *)
    echo "TARGET 只能是 tencent 或 archive，当前值：${TARGET}" >&2
    exit 2
    ;;
esac
