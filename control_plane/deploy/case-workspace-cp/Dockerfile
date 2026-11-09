# Universal Case Workspace (BFF + SPA) for the Control Plane route /apps/case-workspace/.
# Build context: the UCW source tree (deploy.sh copies this file there as Dockerfile.cp).
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
# vite builds with base /apps/case-workspace/ by default for production builds
RUN npm run build

FROM golang:1.25-alpine AS api
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY contracts ./contracts
COPY migrations ./migrations
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/workspace-api ./cmd/workspace-api

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=api /out/workspace-api /app/workspace-api
COPY --from=web /src/web/dist /app/web
COPY domain-packs /app/domain-packs
ENV UCW_WEB_DIR=/app/web \
    UCW_PACKS_DIR=/app/domain-packs
# uid 65532: mounted secret files must be readable by it (deploy.sh: 0644 files inside a 0700 host directory)
USER 65532:65532
ENTRYPOINT ["/app/workspace-api"]
