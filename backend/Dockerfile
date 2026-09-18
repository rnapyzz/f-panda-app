# syntax=docker/dockerfile:1

FROM golang:1.26 AS base
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

# --- dev: hot-reload server for docker compose local development ---
FROM base AS dev
RUN go install github.com/air-verse/air@latest
COPY . .
EXPOSE 8080
CMD ["air", "-c", ".air.toml"]

# --- migrate: goose CLI for schema migrations, run as a one-off compose job ---
FROM base AS migrate
RUN go install github.com/pressly/goose/v3/cmd/goose@latest
COPY db/migrations ./db/migrations
WORKDIR /app/db/migrations
ENTRYPOINT ["goose"]

# --- build: compiles the production binary ---
FROM base AS build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/seed ./cmd/seed

# --- prod: minimal runtime image ---
FROM gcr.io/distroless/static-debian12:nonroot AS prod
COPY --from=build /out/server /server
COPY --from=build /out/seed /seed
COPY db/migrations /db/migrations
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/server"]
