.PHONY: up down logs migrate migrate-status test reset-db

up: ## 開発環境を起動する
	docker compose up --build -d

down: ## 開発環境を停止する
	docker compose down

logs: ## ログを表示する
	docker compose logs -f

migrate: ## マイグレーションを適用する
	docker compose run --rm migrate migrate up

migrate-status: ## マイグレーションの適用状況を表示する
	docker compose run --rm migrate migrate status

test: ## テストを実行する
	cd api && go test ./...
	cd web && npm run lint && npm run build

reset-db: ## DB のデータを削除して作り直す
	docker compose down -v
	docker compose up --build -d
