FROM golang:1.25-alpine AS build
WORKDIR /src

# Build context is the repo root so we can pull in pkg/ alongside lobby/.
# The replace directive in lobby/go.mod resolves pkg via ../pkg.
# GOWORK=off disables any root go.work the developer has locally; the
# build relies on go.mod + replace, not the workspace file.
ENV GOWORK=off
COPY pkg ./pkg
COPY lobby ./lobby

RUN cd lobby && CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/lobby ./cmd/lobby

# --- strip stage: produce the published source tree ---------------------
# Runs the tools/strip allowlist against server/, pkg/, client/ and emits
# the comment-free result into /stripped. The output is copied into the
# runtime stage and served by lobby at /source/*. Doing this at image
# build time ties the published source to the deployed binary version,
# and a broken strip tool fails the image build instead of silently
# leaving the volume empty at deploy time.
FROM golang:1.25-alpine AS strip
WORKDIR /repo
ENV GOWORK=off
COPY server ./server
COPY client ./client
COPY tools ./tools
RUN cd tools/strip && go run . -in /repo -out /stripped \
 && tar -C /stripped -czf /source.tar.gz . \
 && ls -la /source.tar.gz

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && mkdir -p /data
WORKDIR /app
COPY --from=build /out/lobby /app/lobby
COPY --from=strip /source.tar.gz /app/source.tar.gz
EXPOSE 3001
ENTRYPOINT ["/app/lobby"]
