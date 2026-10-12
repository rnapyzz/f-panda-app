# base・build はビルドする側の CPU で動かす（画面のビルド結果は CPU に依存しない。本番は arm64。docs/deploy.md）
FROM --platform=$BUILDPLATFORM node:24-alpine AS base
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

# 本番用: ビルドした画面を nginx で配信する（docs/deploy.md）
FROM nginx:1.28-alpine AS prod
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.prod.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
