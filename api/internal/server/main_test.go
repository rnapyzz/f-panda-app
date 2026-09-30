package server_test

import (
	"os"
	"testing"

	"github.com/rnapyzz/f-panda-app/api/internal/password"
)

func TestMain(m *testing.M) {
	// テストで作るユーザーのパスワードハッシュは反復回数を下げて、テスト時間を短くする。
	password.SetIterationsForTesting(1000)
	os.Exit(m.Run())
}
