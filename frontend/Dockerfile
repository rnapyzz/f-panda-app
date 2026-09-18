# syntax=docker/dockerfile:1

FROM node:22-slim AS base
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

# --- dev: vite dev server with hot module reload for docker compose ---
FROM base AS dev
COPY . .
EXPOSE 5173
CMD ["npm", "run", "dev", "--", "--host"]

# --- build: static assets, copied into the backend image for go:embed ---
FROM base AS build
COPY . .
RUN npm run build
