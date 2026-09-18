.PHONY: dev down migrate migrate-down seed seed-periods sqlc-generate build test lint

## Start MySQL + backend (hot reload) + frontend (vite dev server) in containers.
dev:
	docker compose up --build

down:
	docker compose down

## Apply all pending schema migrations against the dev MySQL container.
migrate:
	docker compose run --rm migrate

## Roll back the most recent migration.
migrate-down:
	docker compose run --rm migrate -dir . mysql "root:root@tcp(mysql:3306)/fpanda?parseTime=true" down

## Create the first office_admin login. Example:
##   make seed EMAIL=admin@example.com NAME="Admin" PASSWORD=changeme
seed:
	docker compose run --rm backend go run ./cmd/seed -email "$(EMAIL)" -name "$(NAME)" -password "$(PASSWORD)"

## (Idempotently) populate dim_period with a rolling fiscal-year window.
## Re-run periodically (e.g. yearly) to extend the window; see cmd/seedperiods
## flags to override the default range or fiscal-year start month.
seed-periods:
	docker compose run --rm backend go run ./cmd/seedperiods

## Regenerate sqlc-generated Go code from backend/db/queries + migrations.
sqlc-generate:
	cd backend && sqlc generate

## Build frontend static assets, copy into backend/web/dist, then build the Go binary.
build:
	cd frontend && npm run build
	rm -rf backend/web/dist
	cp -r frontend/dist backend/web/dist
	cd backend && go build -o ../bin/server ./cmd/server

test:
	cd backend && go test ./...
	cd frontend && npm run build

lint:
	cd backend && go vet ./...
	cd frontend && npm run lint
