// Package codes は、施策・組織・セグメント・ユニットなどのコード（人が指し示すための番号）を扱う。
package codes

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
)

// Pattern はコードの形式。CSV やファイル名でも扱いやすいよう、半角英数字・ハイフン・アンダースコアに限る。
var Pattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,50}$`)

// PatternMessage はコードの形式が正しくないときのメッセージ。
const PatternMessage = "コードは半角英数字・ハイフン・アンダースコアの50文字以内で入力してください"

// Next は table の code 列で、prefix に続く番号の次のコード（例: ORG-0001）を返す。
// taken を指定すると、true を返したコードは使わずに次の番号にする（他のテーブルとの重複を避けるため）。
func Next(ctx context.Context, tx *sql.Tx, table, prefix string, taken func(code string) (bool, error)) (string, error) {
	var maxNo sql.NullInt64
	err := tx.QueryRowContext(ctx,
		"SELECT MAX(CAST(SUBSTRING(code, ?) AS UNSIGNED)) FROM "+table+" WHERE code REGEXP ?",
		len(prefix)+1, "^"+regexp.QuoteMeta(prefix)+"[0-9]+$",
	).Scan(&maxNo)
	if err != nil {
		return "", err
	}
	for n := maxNo.Int64 + 1; ; n++ {
		code := fmt.Sprintf("%s%04d", prefix, n)
		if taken == nil {
			return code, nil
		}
		t, err := taken(code)
		if err != nil {
			return "", err
		}
		if !t {
			return code, nil
		}
	}
}
