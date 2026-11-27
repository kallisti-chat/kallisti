# Kallisti: the Go server with the built web app. See deploy/compose.yml.
# Published for amd64 and arm64 (.github/workflows/image.yml): the web app
# and the server build on the build machine's platform (Go cross-compiles),
# only the last stage runs on the target's.

FROM --platform=$BUILDPLATFORM docker.io/library/node:24-alpine AS web
RUN corepack enable && corepack prepare pnpm@11.22.0 --activate
WORKDIR /src
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml tsconfig.base.json ./
COPY packages/protocol/package.json packages/protocol/
COPY packages/devtools/package.json packages/devtools/
COPY apps/web/package.json apps/web/
RUN pnpm install --frozen-lockfile --filter @kallisti/web...
COPY packages/protocol packages/protocol
COPY apps/web apps/web
RUN pnpm --filter @kallisti/web build

FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.27-alpine AS server
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server ./
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /kallisti ./cmd/server

FROM docker.io/library/alpine:3.21
# ffmpeg: video thumbnails and picture previews (MEDIA_PREVIEWS=0: off).
RUN apk add --no-cache ffmpeg && addgroup -g 1000 kallisti && adduser -D -H -u 1000 -G kallisti kallisti && mkdir -p /data && chown kallisti:kallisti /data
COPY --from=server /kallisti /app/kallisti
COPY --from=web /src/apps/web/dist /app/web
ENV WEB_DIR=/app/web DB_PATH=/data/kallisti.db UPLOAD_DIR=/data/uploads LISTEN_PORT=8080
VOLUME /data
# 8080: web app and web clients; 64738: Mumble clients and the desktop app.
EXPOSE 8080 64738/tcp 64738/udp
USER kallisti
ENTRYPOINT ["/app/kallisti"]
