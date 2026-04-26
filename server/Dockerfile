FROM golang:1.25-alpine AS build
WORKDIR /src

# Build context is the repo root so we can pull in pkg/ alongside server/.
# The replace directive in server/go.mod resolves pkg via ../pkg.
# GOWORK=off keeps the build off the workspace file.
ENV GOWORK=off
COPY pkg ./pkg
COPY server ./server

RUN cd server && CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && mkdir -p /data
WORKDIR /app
COPY --from=build /out/server /app/server
EXPOSE 8080
ENTRYPOINT ["/app/server"]
