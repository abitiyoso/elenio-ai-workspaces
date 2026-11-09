FROM node:22-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY . .
# Mount path when served behind the Control Plane (/apps/flowcase/); "/" for the standalone UI.
ARG FLOWCASE_BASE_PATH=/
ENV FLOWCASE_BASE_PATH=$FLOWCASE_BASE_PATH
RUN npm run build

# Static production build behind nginx; /api, /healthz and /readyz are proxied to the Go API.
FROM nginxinc/nginx-unprivileged:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 8080
