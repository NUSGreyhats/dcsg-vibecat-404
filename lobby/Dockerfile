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

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && mkdir -p /data
WORKDIR /app
COPY --from=build /out/lobby /app/lobby
EXPOSE 3001
ENTRYPOINT ["/app/lobby"]
