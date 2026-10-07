// server は F-Panda の API サーバー。
package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/rnapyzz/f-panda-app/api/internal/auth"
	"github.com/rnapyzz/f-panda-app/api/internal/config"
	"github.com/rnapyzz/f-panda-app/api/internal/notify"
	"github.com/rnapyzz/f-panda-app/api/internal/orgchange"
	"github.com/rnapyzz/f-panda-app/api/internal/server"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	if err := run(logger); err != nil {
		logger.Error("server stopped with error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg := config.Load()

	db, err := sql.Open("mysql", cfg.DB.DSN())
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	authSvc := auth.NewService(db, cfg.SessionTTL)
	// SSO（OIDC）を設定した環境では、パスワードでのログインは FP&A の非常用だけにする
	var oidc *auth.OIDC
	if cfg.OIDC.ClientID != "" {
		oidc = auth.NewOIDC(auth.OIDCConfig(cfg.OIDC))
		authSvc.RestrictPasswordLogin(auth.RoleFPAAdmin)
		logger.Info("sso enabled", "issuer", cfg.OIDC.Issuer, "allowed_domains", cfg.OIDC.AllowedDomains)
	}
	orgChanges := orgchange.NewService(db, orgchange.Options{Logger: logger})
	notifier := notify.NewService(db, notify.Options{SlackWebhookURL: cfg.SlackWebhookURL, BaseURL: cfg.AppBaseURL, Logger: logger})

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: server.NewHandler(server.Deps{
			DB:           db,
			Auth:         authSvc,
			CookieSecure: cfg.CookieSecure,
			TrustProxy:   cfg.TrustProxy,
			OIDC:         oidc,
			Logger:       logger,
			Notifier:     notifier,
			OrgChanges:   orgChanges,
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go cleanupSessions(ctx, authSvc, logger)
	// 締切の前・超過の通知を、送信時刻を過ぎたら送る（docs/plan.md「2.13」）
	go notifier.Run(ctx)
	// 有効日を迎えた組織変更の予約を適用する（docs/plan.md「2.15」）
	go orgChanges.Run(ctx)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// cleanupSessions は期限切れのセッションを1時間ごとに削除する。
func cleanupSessions(ctx context.Context, svc *auth.Service, logger *slog.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := svc.DeleteExpiredSessions(ctx)
			if err != nil {
				logger.Error("cleanup sessions", "error", err)
				continue
			}
			if n > 0 {
				logger.Info("expired sessions deleted", "count", n)
			}
		}
	}
}
