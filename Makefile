SHELL := /bin/bash

.PHONY: up down logs migrate migrate-status create-user test test-api reset-db

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

create-user: ## ユーザーを作成する（例: make create-user EMAIL=admin@example.com NAME=管理者 ROLE=fpa_admin）
	@test -n "$(EMAIL)" -a -n "$(NAME)" || (echo "EMAIL と NAME を指定してください" && exit 1)
	@read -rs -p "パスワード（12文字以上）: " pw; echo; \
	printf '%s\n' "$$pw" | docker compose run --rm -T migrate createuser -email "$(EMAIL)" -name "$(NAME)" -role "$(or $(ROLE),fpa_admin)"

test: test-api ## テストを実行する
	cd web && npm run lint && npm run build

test-api: ## Go のテストを実行する（DB を使うテストは compose の db に接続する）
	cd api && TEST_DB_HOST=127.0.0.1 TEST_DB_PORT=$${DB_PORT_HOST:-3307} TEST_DB_USER=root TEST_DB_PASSWORD=$${MYSQL_ROOT_PASSWORD:-root} go test ./...

reset-db: ## DB のデータを削除して作り直す
	docker compose down -v
	docker compose up --build -d
