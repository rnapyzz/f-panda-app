// Package migrate は埋め込まれた SQL ファイルを MySQL に順番に適用する。
//
// 適用済みのバージョンは schema_migrations テーブルに記録する。
// MySQL の DDL はトランザクションで巻き戻せないため、ファイルの途中で失敗した場合は
// 手動での復旧が必要になる。1ファイルの変更は小さく保つこと。
package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"sort"
	"strings"
)

const lockName = "fpanda_schema_migrations"

// Migration は1つのマイグレーションファイル。
type Migration struct {
	Version string // ファイル名から拡張子を除いたもの（例: 0001_create_masters）
	SQL     string
}

// Load は fsys 直下の *.sql ファイルをバージョン順に読み込む。
func Load(fsys fs.FS) ([]Migration, error) {
	names, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)

	migrations := make([]Migration, 0, len(names))
	for _, name := range names {
		b, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		migrations = append(migrations, Migration{
			Version: strings.TrimSuffix(path.Base(name), ".sql"),
			SQL:     string(b),
		})
	}
	return migrations, nil
}

// Pending は未適用のマイグレーションを返す。
func Pending(all []Migration, applied map[string]bool) []Migration {
	var pending []Migration
	for _, m := range all {
		if !applied[m.Version] {
			pending = append(pending, m)
		}
	}
	return pending
}

// Up は未適用のマイグレーションをすべて適用する。
// 複数プロセスから同時に実行されないよう、MySQL の名前付きロックを取得する。
func Up(ctx context.Context, db *sql.DB, fsys fs.FS, logger *slog.Logger) error {
	all, err := Load(fsys)
	if err != nil {
		return err
	}

	// ロックはセッション単位なので、同じコネクションで処理する。
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	var locked sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 30)", lockName).Scan(&locked); err != nil {
		return fmt.Errorf("get lock: %w", err)
	}
	if !locked.Valid || locked.Int64 != 1 {
		return fmt.Errorf("get lock: timed out")
	}
	defer conn.ExecContext(context.WithoutCancel(ctx), "SELECT RELEASE_LOCK(?)", lockName)

	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version VARCHAR(255) NOT NULL PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := appliedVersions(ctx, conn)
	if err != nil {
		return err
	}

	pending := Pending(all, applied)
	if len(pending) == 0 {
		logger.Info("no pending migrations")
		return nil
	}

	for _, m := range pending {
		logger.Info("applying migration", "version", m.Version)
		if _, err := conn.ExecContext(ctx, m.SQL); err != nil {
			return fmt.Errorf("apply %s: %w", m.Version, err)
		}
		if _, err := conn.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES (?)", m.Version); err != nil {
			return fmt.Errorf("record %s: %w", m.Version, err)
		}
	}
	logger.Info("migrations applied", "count", len(pending))
	return nil
}

// Status は各マイグレーションの適用状況を返す。
func Status(ctx context.Context, db *sql.DB, fsys fs.FS) ([]Migration, map[string]bool, error) {
	all, err := Load(fsys)
	if err != nil {
		return nil, nil, err
	}

	var exists int
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'schema_migrations'",
	).Scan(&exists)
	if err != nil {
		return nil, nil, err
	}
	if exists == 0 {
		return all, map[string]bool{}, nil
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer conn.Close()

	applied, err := appliedVersions(ctx, conn)
	if err != nil {
		return nil, nil, err
	}
	return all, applied, nil
}

func appliedVersions(ctx context.Context, conn *sql.Conn) (map[string]bool, error) {
	rows, err := conn.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("select schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := map[string]bool{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}
