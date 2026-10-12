# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM node:20-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN rm -rf internal/server/dist
COPY --from=web /src/web/dist ./internal/server/dist
ARG TARGETOS
ARG TARGETARCH
RUN set -eu; \
    export CGO_ENABLED=0; \
    if [ -n "${TARGETOS}" ]; then export GOOS="${TARGETOS}"; fi; \
    if [ -n "${TARGETARCH}" ]; then export GOARCH="${TARGETARCH}"; fi; \
    go build -o /out/baidubak -ldflags "-w -s" -trimpath .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
    && mkdir -p /app/runtime/chunks
WORKDIR /app
COPY --from=build /out/baidubak /app/baidubak
EXPOSE 8080
ENTRYPOINT ["/app/baidubak"]
CMD ["serve", "--addr", "127.0.0.1:8080"]
