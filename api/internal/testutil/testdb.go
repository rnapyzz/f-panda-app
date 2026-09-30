// Package testutil はテスト用のヘルパーを提供する。
package testutil

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/go-sql-driver/mysql"

	"github.com/rnapyzz/f-panda-app/api/internal/migrate"
	"github.com/rnapyzz/f-panda-app/api/migrations"
)

// NewDB はテスト専用のデータベースを作成してマイグレーションを適用し、接続を返す。
// データベースはテスト終了時に削除される。
//
// 接続先は環境変数 TEST_DB_HOST / TEST_DB_PORT / TEST_DB_USER / TEST_DB_PASSWORD で指定する。
// ユーザーにはデータベースを作成・削除する権限が必要。TEST_DB_HOST が未設定の場合はテストをスキップする。
func NewDB(t testing.TB) *sql.DB {
	t.Helper()

	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		t.Skip("TEST_DB_HOST が未設定のため DB を使うテストをスキップします")
	}

	cfg := mysql.NewConfig()
	cfg.Net = "tcp"
	cfg.Addr = host + ":" + getenv("TEST_DB_PORT", "3306")
	cfg.User = getenv("TEST_DB_USER", "root")
	cfg.Passwd = getenv("TEST_DB_PASSWORD", "root")
	cfg.ParseTime = true
	cfg.Params = map[string]string{"charset": "utf8mb4"}

	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })

	name := "fpanda_test_" + randomSuffix(t)
	ctx := context.Background()
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+name+" CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.ExecContext(context.Background(), "DROP DATABASE "+name); err != nil {
			t.Errorf("drop test database: %v", err)
		}
	})

	migCfg := cfg.Clone()
	migCfg.DBName = name
	migCfg.MultiStatements = true
	migDB, err := sql.Open("mysql", migCfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer migDB.Close()
	if err := migrate.Up(ctx, migDB, migrations.FS, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	appCfg := cfg.Clone()
	appCfg.DBName = name
	db, err := sql.Open("mysql", appCfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func randomSuffix(t testing.TB) string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
