# syntax=docker/dockerfile:1.7
# bodega-api: Go API + the SvelteKit SPA as static files. Build context: repo root.

FROM node:22-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-fund --no-audit
COPY web/ ./
RUN npm run build

FROM golang:1.27 AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd/ cmd/
COPY internal/ internal/
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/bodega-api ./cmd/bodega-api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=go /out/bodega-api /bodega-api
COPY --from=web /src/web/build /web
ENV BODEGA_WEB_DIR=/web
EXPOSE 8080
ENTRYPOINT ["/bodega-api"]
