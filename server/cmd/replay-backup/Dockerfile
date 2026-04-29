# Multi-stage build for the localhost replay-backup tool.
# Build context is the repo root (so we can pull in pkg/ + server/ + client/).
#
#   docker build -f server/cmd/replay-backup/Dockerfile -t replay-backup:local .
#   docker run --rm -p 8080:8080 replay-backup:local

# --- Stage 1: build the Vite client bundle. ---
FROM node:22-alpine AS client-build
WORKDIR /src
COPY client/package.json client/yarn.lock ./
RUN yarn install --frozen-lockfile
COPY client/ ./
RUN yarn build

# --- Stage 2: build the Go binary. ---
FROM golang:1.25-alpine AS go-build
WORKDIR /src
ENV GOWORK=off
COPY pkg ./pkg
COPY server ./server
RUN cd server && CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/replay-backup ./cmd/replay-backup

# --- Stage 3: runtime. ---
FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=go-build /out/replay-backup /app/replay-backup
COPY --from=client-build /src/dist /app/dist
EXPOSE 8080
# Bind 0.0.0.0 inside the container — the docker -p mapping handles the
# host-side localhost binding. -client-dist points at the bundle copied
# from the client-build stage.
ENTRYPOINT ["/app/replay-backup", "-addr=0.0.0.0:8080", "-client-dist=/app/dist"]
