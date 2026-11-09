# Full build (CI / registry builds). The VM deploy ships a prebuilt binary via deploy/Dockerfile.runtime.
FROM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS api
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/control-plane-api ./cmd/control-plane-api

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=api /out/control-plane-api /app/control-plane-api
COPY --from=web /web/dist /app/web/dist
COPY deploy/dev-seed.json /app/deploy/dev-seed.json
ENV WEB_DIR=/app/web/dist DEV_SEED_FILE=/app/deploy/dev-seed.json
USER nonroot:nonroot
ENTRYPOINT ["/app/control-plane-api"]
CMD ["serve"]
