# syntax=docker/dockerfile:1

FROM node:22-bookworm-slim AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/yarn.lock ./
RUN corepack enable && yarn install --frozen-lockfile --ignore-engines
COPY frontend/ ./
RUN yarn build

FROM golang:1.23-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
COPY --from=frontend /src/dist ./dist
ARG TARGETOS=linux
ARG TARGETARCH
ARG TARGETVARIANT
ARG VERSION=dev
RUN set -eu; \
  GOARM_VALUE=""; \
  if [ "$TARGETARCH" = "arm" ] && [ -n "${TARGETVARIANT:-}" ]; then GOARM_VALUE="${TARGETVARIANT#v}"; fi; \
  CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" GOARM="$GOARM_VALUE" \
  go build -trimpath -ldflags "-X main.Version=$VERSION -s -w" -o /out/albs .

FROM node:22-bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      git openssh-client ca-certificates bash python3 make g++ gosu \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --uid 10001 --shell /bin/bash albs \
    && corepack enable
WORKDIR /app
COPY --from=builder /out/albs /usr/local/bin/albs
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint
RUN mkdir -p /workspace/robots /home/albs/.ssh /home/albs/.config \
    && chown -R albs:albs /workspace /home/albs \
    && chmod +x /usr/local/bin/docker-entrypoint
ENV HOME=/home/albs \
    XDG_CONFIG_HOME=/home/albs/.config \
    PORT=17390 \
    ALBS_BIND=0.0.0.0 \
    ALEMONJS_SETUP_ROOTS=/workspace/robots
VOLUME ["/workspace", "/home/albs/.config", "/home/albs/.ssh"]
EXPOSE 17390
ENTRYPOINT ["docker-entrypoint"]
CMD ["albs", "serve", "--port", "17390"]
