FROM node:22-alpine AS web
WORKDIR /src
COPY apps/agent-studio-web/package.json apps/agent-studio-web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY apps/agent-studio-web/ ./
# "/" by default; "/apps/agent-studio/" when served behind the Control Plane.
ARG AGENT_STUDIO_WEB_BASE_PATH=/
RUN AGENT_STUDIO_WEB_BASE_PATH="$AGENT_STUDIO_WEB_BASE_PATH" npm run build

FROM golang:1.25-alpine AS api
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/agent-studio ./cmd/agent-studio

FROM alpine:3.22
RUN addgroup -S -g 10001 agentstudio && adduser -S -D -H -u 10001 -G agentstudio agentstudio \
    && apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=api --chown=10001:10001 /out/agent-studio /app/agent-studio
COPY --from=web --chown=10001:10001 /src/dist /app/web
ENV AGENT_STUDIO_HTTP_ADDR=127.0.0.1:8190 AGENT_STUDIO_WEB_DIR=/app/web
EXPOSE 8190
USER 10001:10001
ENTRYPOINT ["/app/agent-studio"]
