package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"hopper/backend/internal/bootstrap"
	"hopper/backend/internal/config"
	"hopper/backend/internal/db"
	"hopper/backend/internal/httpapi"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}

	database, err := db.Open(cfg.DatabasePath)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	if err := db.Migrate(database); err != nil {
		logger.Error("run migrations", "error", err)
		os.Exit(1)
	}

	bootstrapResult, err := bootstrap.EnsureFirstUser(context.Background(), cfg, database)
	if err != nil {
		logger.Error("first-run bootstrap", "error", err)
		os.Exit(1)
	}
	if bootstrapResult.Created {
		if bootstrapResult.PasswordGenerated {
			logger.Warn("first-run admin user created; save this password now because it will not be shown again", "login", bootstrapResult.Login, "password", bootstrapResult.Password)
		} else {
			logger.Info("first-run admin user created from FIRST_RUN_ADMIN_PASSWORD", "login", bootstrapResult.Login)
		}
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.NewRouter(cfg, database),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("starting server", "addr", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown server", "error", err)
		os.Exit(1)
	}
}
