package auth

import (
	"context"
	"errors"
	"time"
)

// ログイン失敗の制限（docs/architecture.md「パスワードでのログイン」）。
// 同じメールアドレス、または同じ IP からの失敗が throttleWindow に throttleLimit 回に達したら、ログインを止める。

const (
	throttleLimit  = 5
	throttleWindow = 15 * time.Minute
	// attemptsKeep はログインの失敗の記録を残す期間
	attemptsKeep = 24 * time.Hour
)

// ErrThrottled はログインの失敗が多すぎて、ログインを止めているときに返る。
var ErrThrottled = errors.New("auth: too many failed attempts")

// throttled は、メールアドレスか IP の失敗が上限に達しているかを返す。
func (s *Service) throttled(ctx context.Context, email, ip string) (bool, error) {
	since := s.now().Add(-throttleWindow)
	var byEmail, byIP int
	err := s.db.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM login_attempts WHERE email = ? AND attempted_at > ?),
		       (SELECT COUNT(*) FROM login_attempts WHERE ip = ? AND attempted_at > ?)`,
		email, since, ip, since).Scan(&byEmail, &byIP)
	if err != nil {
		return false, err
	}
	return byEmail >= throttleLimit || byIP >= throttleLimit, nil
}

func (s *Service) recordFailure(ctx context.Context, email, ip string) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO login_attempts (email, ip, attempted_at) VALUES (?, ?, ?)", email, ip, s.now())
	return err
}

func (s *Service) clearFailures(ctx context.Context, email string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM login_attempts WHERE email = ?", email)
	return err
}

// deleteOldAttempts は古いログインの失敗の記録を消す。
func (s *Service) deleteOldAttempts(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM login_attempts WHERE attempted_at <= ?", s.now().Add(-attemptsKeep))
	return err
}
