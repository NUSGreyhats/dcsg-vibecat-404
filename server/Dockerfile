FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && mkdir -p /data
WORKDIR /app
COPY --from=build /out/server /app/server
EXPOSE 8080
ENTRYPOINT ["/app/server"]
