FROM node:24-alpine AS base
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

# 開発用: Vite dev server
FROM base AS dev
COPY . .
EXPOSE 5173
CMD ["npm", "run", "dev"]

# 本番ビルド
FROM base AS build
COPY . .
RUN npm run build
