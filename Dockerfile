# syntax=docker/dockerfile:1
# Three stages: build the UI, build a static Go binary with the UI embedded, and
# ship only that binary on an empty image (CLAUDE.md §12).

FROM --platform=$BUILDPLATFORM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
# vite writes into ../internal/webui/dist
RUN mkdir -p /src/internal/webui/dist && npm run build

FROM alpine:3 AS certs
RUN apk add --no-cache ca-certificates

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ENV GOTOOLCHAIN=local CGO_ENABLED=0
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/webui/dist internal/webui/dist
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags="-s -w -X main.version=${VERSION}" -o /out/mashov2go ./cmd/mashov2go
RUN mkdir -p /out/data && chown 65532:65532 /out/data

FROM scratch
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/mashov2go /mashov2go
COPY --from=build --chown=65532:65532 /out/data /data
USER 65532:65532
ENV MASHOV2GO_DATA_DIR=/data MASHOV2GO_LISTEN=:8080
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/mashov2go", "--version"]
ENTRYPOINT ["/mashov2go"]
CMD ["run"]
