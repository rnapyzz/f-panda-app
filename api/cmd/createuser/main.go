// createuser はユーザーを作成する。最初の FP&A 管理者を作るために使う。
//
// パスワードは標準入力の1行目から読み込む（コマンドライン引数に残さないため）。
//
//	printf '%s' "$PASSWORD" | createuser -email admin@example.com -name 管理者 -role fpa_admin
package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/rnapyzz/f-panda-app/api/internal/auth"
	"github.com/rnapyzz/f-panda-app/api/internal/config"
	"github.com/rnapyzz/f-panda-app/api/internal/password"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	email := flag.String("email", "", "メールアドレス（必須）")
	name := flag.String("name", "", "氏名（必須）")
	role := flag.String("role", string(auth.RoleFPAAdmin), "ロール: fpa_admin / manager / member / viewer")
	flag.Parse()

	if *email == "" || *name == "" {
		flag.Usage()
		return errors.New("-email と -name は必須です")
	}
	if !auth.Role(*role).Valid() {
		return fmt.Errorf("不明なロールです: %s", *role)
	}

	pw, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && pw == "" {
		return errors.New("標準入力からパスワードを読み込めませんでした")
	}
	pw = strings.TrimRight(pw, "\r\n")
	if err := password.Validate(pw); err != nil {
		return err
	}
	hash, err := password.Hash(pw)
	if err != nil {
		return err
	}

	db, err := sql.Open("mysql", config.Load().DB.DSN())
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := db.ExecContext(ctx,
		"INSERT INTO users (name, email, password_hash, role) VALUES (?, ?, ?, ?)",
		strings.TrimSpace(*name), auth.NormalizeEmail(*email), hash, *role,
	)
	var myErr *mysql.MySQLError
	if errors.As(err, &myErr) && myErr.Number == 1062 {
		return fmt.Errorf("このメールアドレスは登録済みです: %s", *email)
	}
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	fmt.Printf("ユーザーを作成しました: id=%d email=%s role=%s\n", id, auth.NormalizeEmail(*email), *role)
	return nil
}
