#!/usr/bin/env sh
set -eu

IMAGE_NAME=${IMAGE_NAME:-alemonx}
VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}
PLATFORM=${PLATFORM:-linux/amd64}
ARCHIVE=${ARCHIVE:-dist/alx-${VERSION}-${PLATFORM#linux/}.oci.tar}
BUILDER=${BUILDER:-alx-builder}

mkdir -p "$(dirname "$ARCHIVE")"
if ! docker buildx inspect "$BUILDER" >/dev/null 2>&1; then
  docker buildx create --name "$BUILDER" --use >/dev/null
else
  docker buildx use "$BUILDER"
fi

echo "构建 ${IMAGE_NAME}:${VERSION} (${PLATFORM})"
docker buildx build \
  --platform "$PLATFORM" \
  --build-arg "VERSION=$VERSION" \
  --tag "${IMAGE_NAME}:${VERSION}" \
  --output "type=oci,dest=${ARCHIVE},name=${IMAGE_NAME}:${VERSION}" \
  .

echo "离线镜像已生成：${ARCHIVE}"
echo "服务器导入：docker load -i ${ARCHIVE}"
