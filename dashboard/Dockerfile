# ── Stage 1: Build ──────────────────────────────────────────
FROM node:22-alpine AS builder

WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

# ── Stage 2: Serve ──────────────────────────────────────────
FROM nginxinc/nginx-unprivileged:1.27-alpine

# Copy built assets
COPY --from=builder /app/dist /usr/share/nginx/html

# Copy nginx config as template (for env substitution)
COPY nginx.conf /etc/nginx/templates/default.conf.template

# Default env vars for the API proxy (overridden by Kubernetes)
ENV GATEWAY_HOST=gateway \
    GATEWAY_PORT=8080

EXPOSE 8080
