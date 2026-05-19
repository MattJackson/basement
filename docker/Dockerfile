# syntax=docker/dockerfile:1.7

# Stage 1 — frontend build
FROM node:22-alpine AS frontend
WORKDIR /src
RUN corepack enable && corepack prepare pnpm@latest --activate
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
# Vite's prebuild reads ../openapi/basement.yaml; mirror that layout.
COPY openapi /openapi
COPY frontend/ ./
RUN pnpm build
# Vite writes to ../internal/web/dist (= /internal/web/dist in container).

# Stage 2 — backend build (embeds the frontend dist)
FROM golang:1.25-alpine AS backend
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /internal/web/dist /src/internal/web/dist
ARG TARGETOS
ARG TARGETARCH
# Strip + static + trimpath = smallest reproducible binary.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -tags 'osusergo netgo' \
    -ldflags="-s -w -buildid=" \
    -buildvcs=false \
    -o /out/basement ./cmd/basement-server

# Stage 3 — runtime: FROM scratch for the smallest possible image.
# We copy in the CA certs (for TLS to Garage/OIDC) and a minimal
# passwd entry so the binary can run as a non-root user.
FROM scratch
COPY --from=backend /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=backend /etc/passwd /etc/passwd
COPY --from=backend /out/basement /basement
# uid:gid 65532:65532 = nobody-equivalent across distroless and Alpine.
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/basement"]
