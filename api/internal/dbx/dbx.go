// Package dbx は database/sql と MySQL を扱う小さな共通処理を提供する。
package dbx

import (
	"context"
	"database/sql"
	"errors"

	"github.com/go-sql-driver/mysql"
)

// Querier は *sql.DB と *sql.Tx に共通する1行取得のメソッド。
type Querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// MySQL のエラー番号
const (
	ErrDuplicateEntry  = 1062
	ErrRowIsReferenced = 1451
)

// ErrNo は MySQL のエラー番号を返す。MySQL のエラーでなければ 0。
func ErrNo(err error) uint16 {
	var myErr *mysql.MySQLError
	if errors.As(err, &myErr) {
		return myErr.Number
	}
	return 0
}

// Count は COUNT(*) を返すクエリを実行する。
func Count(ctx context.Context, q Querier, query string, args ...any) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, query, args...).Scan(&n)
	return n, err
}

// Exists は table に id のレコードが存在するかを返す。tx 内で FOR SHARE ロックを取る。
func Exists(ctx context.Context, tx *sql.Tx, table string, id int64) (bool, error) {
	var one int
	err := tx.QueryRowContext(ctx, "SELECT 1 FROM "+table+" WHERE id = ? FOR SHARE", id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// NullInt64 は *int64 を sql.NullInt64 に変換する。
func NullInt64(p *int64) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *p, Valid: true}
}

// PtrInt64 は sql.NullInt64 を *int64 に変換する。
func PtrInt64(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

// NullString は空文字列を NULL として扱う。
func NullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
