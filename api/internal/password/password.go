// Package password はパスワードのハッシュ化と検証を行う。
//
// 標準ライブラリの PBKDF2-HMAC-SHA256 を使う。反復回数は OWASP の推奨値（600,000回）。
// ハッシュは "pbkdf2-sha256$<反復回数>$<salt>$<hash>"（salt と hash は base64）の形式で保存する。
package password

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	scheme     = "pbkdf2-sha256"
	iterations = 600_000
	saltLen    = 16
	keyLen     = 32

	// MinLength はパスワードの最小文字数。
	MinLength = 12
	// MaxLength はパスワードの最大文字数。
	MaxLength = 128
)

// ErrInvalidHash は保存されたハッシュの形式が不正なときに返る。
var ErrInvalidHash = errors.New("password: invalid hash format")

var b64 = base64.RawStdEncoding

// Validate はパスワードの要件を満たしているかを検証する。
func Validate(pw string) error {
	n := utf8.RuneCountInString(pw)
	if n < MinLength {
		return fmt.Errorf("パスワードは%d文字以上にしてください", MinLength)
	}
	if n > MaxLength {
		return fmt.Errorf("パスワードは%d文字以下にしてください", MaxLength)
	}
	return nil
}

// Hash はパスワードをハッシュ化する。
func Hash(pw string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, pw, salt, iterations, keyLen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s$%d$%s$%s", scheme, iterations, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// Verify はパスワードがハッシュと一致するかを返す。
func Verify(pw, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != scheme {
		return false, ErrInvalidHash
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter <= 0 {
		return false, ErrInvalidHash
	}
	salt, err := b64.DecodeString(parts[2])
	if err != nil {
		return false, ErrInvalidHash
	}
	want, err := b64.DecodeString(parts[3])
	if err != nil {
		return false, ErrInvalidHash
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iter, len(want))
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
