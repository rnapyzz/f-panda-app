// migrate はデータベースのマイグレーションを実行する。
//
// 使い方:
//
//	migrate up      未適用のマイグレーションを適用する（デフォルト）
//	migrate status  適用状況を表示する
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/rnapyzz/f-panda-app/api/internal/config"
	"github.com/rnapyzz/f-panda-app/api/internal/migrate"
	"github.com/rnapyzz/f-panda-app/api/migrations"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("migrate failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cmd := "up"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	cfg := config.Load()
	db, err := sql.Open("mysql", cfg.DB.MigrationDSN())
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect database: %w", err)
	}

	switch cmd {
	case "up":
		return migrate.Up(ctx, db, migrations.FS, logger)
	case "status":
		all, applied, err := migrate.Status(ctx, db, migrations.FS)
		if err != nil {
			return err
		}
		for _, m := range all {
			state := "pending"
			if applied[m.Version] {
				state = "applied"
			}
			fmt.Printf("%-8s %s\n", state, m.Version)
		}
		return nil
	default:
		return fmt.Errorf("unknown command %q (use: up, status)", cmd)
	}
}
