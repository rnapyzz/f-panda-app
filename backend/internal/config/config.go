package config

import (
	"fmt"
	"os"
)

// Config holds runtime configuration loaded from environment variables.
type Config struct {
	Addr         string
	DBDSN        string
	CookieSecure bool
}

func Load() (Config, error) {
	cfg := Config{
		Addr:         getEnv("APP_ADDR", ":8080"),
		CookieSecure: getEnv("APP_COOKIE_SECURE", "true") != "false",
	}

	dsn := os.Getenv("APP_DB_DSN")
	if dsn == "" {
		host := getEnv("APP_DB_HOST", "127.0.0.1")
		port := getEnv("APP_DB_PORT", "3306")
		user := getEnv("APP_DB_USER", "fpanda")
		pass := getEnv("APP_DB_PASSWORD", "fpanda")
		name := getEnv("APP_DB_NAME", "fpanda")
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&multiStatements=false", user, pass, host, port, name)
	}
	cfg.DBDSN = dsn

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
