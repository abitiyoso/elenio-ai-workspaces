# KMS frontend for the Control Plane route: built with base /apps/kms/, served by nginx, /api proxied to the KMS API.
FROM node:22-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY . .
ARG KMS_BASE_PATH=/apps/kms/
ENV KMS_BASE_PATH=$KMS_BASE_PATH
RUN npx vite build

FROM nginxinc/nginx-unprivileged:1.27-alpine
COPY nginx.cp.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 8080
