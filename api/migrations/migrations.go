// Package migrations は SQL マイグレーションファイルを埋め込んで提供する。
//
// ファイル名は "<連番>_<説明>.sql"（例: 0001_create_masters.sql）とし、
// 連番の昇順に適用される。適用済みのファイルは変更せず、変更は新しいファイルで行う。
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
