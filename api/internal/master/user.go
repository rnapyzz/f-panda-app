package master

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/auth"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
	"github.com/rnapyzz/f-panda-app/api/internal/password"
)

// user はユーザー。パスワードハッシュはレスポンスにも監査ログにも含めない。
type user struct {
	ID       int64     `json:"id"`
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Role     auth.Role `json:"role"`
	IsActive bool      `json:"is_active"`
	// SlackUserID は Slack のメンバー ID（通知のメンション用、docs/plan.md「2.13」）。空は未登録
	SlackUserID string `json:"slack_user_id"`
	// HasPassword は、パスワードが設定されているか（CSV で追加したユーザーは未設定）
	HasPassword bool `json:"has_password"`
	timestamps
}

type userCreateRequest struct {
	Name        string    `json:"name"`
	Email       string    `json:"email"`
	Role        auth.Role `json:"role"`
	Password    string    `json:"password"`
	SlackUserID string    `json:"slack_user_id"`
	reasonRequest
}

type userUpdateRequest struct {
	Name        string    `json:"name"`
	Email       string    `json:"email"`
	Role        auth.Role `json:"role"`
	IsActive    bool      `json:"is_active"`
	SlackUserID string    `json:"slack_user_id"`
	reasonRequest
}

type passwordRequest struct {
	Password string `json:"password"`
	reasonRequest
}

const userSelect = "SELECT id, name, email, role, is_active, COALESCE(slack_user_id, ''), password_hash <> '" + unusablePasswordHash + "', created_at, updated_at FROM users"

func scanUser(row interface{ Scan(...any) error }) (user, error) {
	var u user
	err := row.Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.IsActive, &u.SlackUserID, &u.HasPassword, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

func findUser(ctx context.Context, q dbx.Querier, id int64, lock string) (user, error) {
	u, err := scanUser(q.QueryRowContext(ctx, userSelect+" WHERE id = ?"+lock, id))
	if errors.Is(err, sql.ErrNoRows) {
		return user{}, notFound("ユーザー")
	}
	return u, err
}

// listUsers は GET /api/users。
func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) error {
	rows, err := h.db.QueryContext(r.Context(), userSelect+" ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()

	var items []user
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return err
		}
		items = append(items, u)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.WriteList(w, items)
	return nil
}

// getUser は GET /api/users/{id}。
func (h *Handler) getUser(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	u, err := findUser(r.Context(), h.db, id, "")
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, u)
	return nil
}

// createUser は POST /api/users。
func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) error {
	var req userCreateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := httpx.Validator{}
	name, email := validateUserFields(v, req.Name, req.Email, req.Role)
	slackID := validateSlackID(v, req.SlackUserID)
	if err := password.Validate(req.Password); err != nil {
		v.Add("password", err.Error())
	}
	if err := v.Err(); err != nil {
		return err
	}
	hash, err := password.Hash(req.Password)
	if err != nil {
		return err
	}

	ctx := r.Context()
	var created user
	err = inTx(ctx, h.db, r, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		res, err := tx.ExecContext(ctx,
			"INSERT INTO users (name, email, password_hash, role, slack_user_id) VALUES (?, ?, ?, ?, ?)",
			name, email, hash, req.Role, dbx.NullString(slackID),
		)
		if dbx.ErrNo(err) == dbx.ErrDuplicateEntry {
			return httpx.Validation(map[string]string{"email": "このメールアドレスは既に登録されています"})
		}
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if created, err = findUser(ctx, tx, id, ""); err != nil {
			return err
		}
		return rec.Insert(ctx, "users", id, created)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, created)
	return nil
}

// updateUser は PUT /api/users/{id}。氏名・メールアドレス・ロール・有効/無効を更新する。
// 自分自身のロール変更と無効化はできない（FP&A 管理者が誰もいなくなるのを防ぐため）。
func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req userUpdateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := httpx.Validator{}
	name, email := validateUserFields(v, req.Name, req.Email, req.Role)
	slackID := validateSlackID(v, req.SlackUserID)
	if err := v.Err(); err != nil {
		return err
	}

	me, _ := auth.UserFrom(r.Context())
	ctx := r.Context()
	var updated user
	err = inTx(ctx, h.db, r, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		before, err := findUser(ctx, tx, id, " FOR UPDATE")
		if err != nil {
			return err
		}
		if id == me.ID {
			if req.Role != before.Role {
				return httpx.Validation(map[string]string{"role": "自分自身のロールは変更できません"})
			}
			if !req.IsActive {
				return httpx.Validation(map[string]string{"is_active": "自分自身は無効化できません"})
			}
		}
		_, err = tx.ExecContext(ctx,
			"UPDATE users SET name = ?, email = ?, role = ?, is_active = ?, slack_user_id = ? WHERE id = ?",
			name, email, req.Role, req.IsActive, dbx.NullString(slackID), id,
		)
		if dbx.ErrNo(err) == dbx.ErrDuplicateEntry {
			return httpx.Validation(map[string]string{"email": "このメールアドレスは既に登録されています"})
		}
		if err != nil {
			return err
		}
		// 無効化したユーザーのセッションは削除する。
		if !req.IsActive {
			if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", id); err != nil {
				return err
			}
		}
		if updated, err = findUser(ctx, tx, id, ""); err != nil {
			return err
		}
		return rec.Update(ctx, "users", id, before, updated)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// resetPassword は PUT /api/users/{id}/password。FP&A がパスワードを再設定する。
// 対象ユーザーの既存セッションはすべて削除する。監査ログにはパスワードを変更した事実のみ残す。
func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req passwordRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if err := password.Validate(req.Password); err != nil {
		return httpx.Validation(map[string]string{"password": err.Error()})
	}
	hash, err := password.Hash(req.Password)
	if err != nil {
		return err
	}

	ctx := r.Context()
	err = inTx(ctx, h.db, r, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		if _, err := findUser(ctx, tx, id, " FOR UPDATE"); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE users SET password_hash = ? WHERE id = ?", hash, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", id); err != nil {
			return err
		}
		return rec.Update(ctx, "users", id, nil, map[string]bool{"password_changed": true})
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// slackIDPattern は Slack のメンバー ID の形式（例: U012AB3CD）。
var slackIDPattern = regexp.MustCompile(`^[UW][A-Z0-9]{2,19}$`)

// validateSlackID は Slack のメンバー ID を検証する。空は未登録。
func validateSlackID(v httpx.Validator, s string) string {
	s = strings.TrimSpace(s)
	if s != "" && !slackIDPattern.MatchString(s) {
		v.Add("slack_user_id", "Slack のメンバー ID は U または W で始まる英大文字・数字で入力してください（例: U012AB3CD）")
	}
	return s
}

func validateUserFields(v httpx.Validator, name, email string, role auth.Role) (string, string) {
	name = v.Text("name", "氏名", name, maxNameLen)
	email = auth.NormalizeEmail(v.Text("email", "メールアドレス", email, 255))
	if email != "" && !validEmail(email) {
		v.Add("email", "メールアドレスの形式が正しくありません")
	}
	if !role.Valid() {
		v.Add("role", "ロールは fpa_admin / manager / member / viewer のいずれかを指定してください")
	}
	return name, email
}

// validEmail はメールアドレスの最低限の形式（ローカル部@ドメイン.xx）を確認する。
func validEmail(s string) bool {
	local, domain, ok := strings.Cut(s, "@")
	return ok && local != "" && strings.Count(s, "@") == 1 && strings.Contains(domain, ".") &&
		!strings.ContainsAny(s, " \t\r\n") && !strings.HasPrefix(domain, ".") && !strings.HasSuffix(domain, ".")
}
