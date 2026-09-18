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

	"github.com/rnapyzz/f-panda-app/backend/internal/auth"
	"github.com/rnapyzz/f-panda-app/backend/internal/config"
	"github.com/rnapyzz/f-panda-app/backend/internal/db"
	"github.com/rnapyzz/f-panda-app/backend/internal/dimension"
	"github.com/rnapyzz/f-panda-app/backend/internal/fact"
	"github.com/rnapyzz/f-panda-app/backend/internal/httpapi"
	"github.com/rnapyzz/f-panda-app/backend/internal/inputsheet"
	"github.com/rnapyzz/f-panda-app/backend/internal/period"
	"github.com/rnapyzz/f-panda-app/backend/internal/scenario"
	"github.com/rnapyzz/f-panda-app/backend/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	sqlDB, err := sql.Open("mysql", cfg.DBDSN)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(20)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	pingCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		return err
	}

	queries := db.New(sqlDB)
	authSvc := auth.NewService(queries, cfg.CookieSecure)
	dimensionSvc := dimension.NewService(queries)
	periodSvc := period.NewService(queries)
	scenarioSvc := scenario.NewService(sqlDB, queries)
	factSvc := fact.NewService(queries, scenarioSvc)
	inputSheetSvc := inputsheet.NewService(sqlDB, queries)

	spaHandler, err := web.Handler()
	if err != nil {
		return err
	}
	router := httpapi.NewRouter(httpapi.Deps{
		Auth:        authSvc,
		Dimensions:  dimensionSvc,
		Periods:     periodSvc,
		Scenarios:   scenarioSvc,
		Facts:       factSvc,
		InputSheets: inputSheetSvc,
		SPAHandler:  spaHandler,
	})

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("server listening", "addr", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutting down")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	return srv.Shutdown(shutdownCtx)
}
