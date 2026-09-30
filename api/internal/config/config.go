// Package config は環境変数からアプリケーション設定を読み込む。
package config

import (
	"os"
	"time"

	"github.com/go-sql-driver/mysql"
)

// Config はアプリケーション設定。
type Config struct {
	HTTPAddr string
	DB       DBConfig
}

// DBConfig は MySQL の接続設定。
type DBConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
}

// Load は環境変数から設定を読み込む。未設定の項目は開発用のデフォルト値を使う。
func Load() Config {
	return Config{
		HTTPAddr: getenv("HTTP_ADDR", ":8080"),
		DB: DBConfig{
			Host:     getenv("DB_HOST", "localhost"),
			Port:     getenv("DB_PORT", "3306"),
			User:     getenv("DB_USER", "fpanda"),
			Password: getenv("DB_PASSWORD", "fpanda"),
			Name:     getenv("DB_NAME", "fpanda"),
		},
	}
}

// DSN はアプリケーション用の接続文字列を返す。
func (c DBConfig) DSN() string {
	return c.dsn(false)
}

// MigrationDSN はマイグレーション用の接続文字列を返す。
// マイグレーションファイルは複数の SQL 文を含むため、複数文の実行を有効にする。
func (c DBConfig) MigrationDSN() string {
	return c.dsn(true)
}

func (c DBConfig) dsn(multiStatements bool) string {
	jst, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		jst = time.FixedZone("Asia/Tokyo", 9*60*60)
	}

	cfg := mysql.NewConfig()
	cfg.Net = "tcp"
	cfg.Addr = c.Host + ":" + c.Port
	cfg.User = c.User
	cfg.Passwd = c.Password
	cfg.DBName = c.Name
	cfg.ParseTime = true
	cfg.Loc = jst
	cfg.MultiStatements = multiStatements
	cfg.Params = map[string]string{"charset": "utf8mb4"}
	return cfg.FormatDSN()
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
