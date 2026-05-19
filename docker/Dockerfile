# syntax=docker/dockerfile:1.7

# Stage 1 — frontend build
FROM node:22-alpine AS frontend
WORKDIR /src
RUN corepack enable && corepack prepare pnpm@latest --activate
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
# Copy openapi as sibling to src so ../openapi/basement.yaml resolves
# from within frontend/ (WORKDIR is /src, so ../openapi = /openapi)
RUN mkdir -p /openapi && COPY openapi/basement.yaml /openapi/
COPY frontend/ ./
RUN pnpm build

# Stage 2 — backend build (embeds the frontend dist)
FROM golang:1.22-alpine AS backend
WORKDIR /src
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/dist /src/internal/web/dist
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/basement ./cmd/basement-server

# Stage 3 — runtime (~15MB total)
FROM gcr.io/distroless/static:nonroot
COPY --from=backend /out/basement /basement
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/basement"]
