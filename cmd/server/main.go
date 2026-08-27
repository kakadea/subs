package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/kakadea/subs/internal/config"
	"github.com/kakadea/subs/internal/httpapp"
	"github.com/kakadea/subs/internal/mal"
	"github.com/kakadea/subs/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if len(os.Args) > 1 && os.Args[1] == "admin-password" {
		if err := changeAdminPassword(logger, os.Args[2:]); err != nil {
			logger.Error("admin password change failed", "error", err)
			os.Exit(1)
		}
		return
	}
	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	db, err := openDatabase(cfg.DatabaseDSN, logger)
	if err != nil {
		logger.Error("database unavailable", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	st := store.New(db)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := st.Migrate(ctx); err != nil {
		logger.Error("migration failed", "error", err)
		os.Exit(1)
	}
	adminEmail := os.Getenv("ADMIN_EMAIL")
	adminPassword := os.Getenv("ADMIN_PASSWORD")
	if err := st.EnsureAdmin(ctx, adminEmail, adminPassword); err != nil {
		logger.Error("admin bootstrap failed", "error", err)
		os.Exit(1)
	}

	app := httpapp.New(cfg, st, logger, mal.NewClient(nil, os.Getenv("METADATA_API_BASE_URL")))
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       70 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("server started", "addr", cfg.Addr, "env", cfg.AppEnv)
		serverErr <- server.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-stop:
		logger.Info("shutdown signal received", "signal", sig.String())
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
	logger.Info("server stopped")
}

func changeAdminPassword(logger *slog.Logger, args []string) error {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return fmt.Errorf("usage: subs admin-password ADMIN_EMAIL; read the new password from stdin")
	}
	password, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read password from stdin: %w", err)
	}
	password = bytes.TrimRight(password, "\r\n")
	if len(password) < 12 {
		return fmt.Errorf("password must contain at least 12 characters")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	db, err := openDatabase(cfg.DatabaseDSN, logger)
	if err != nil {
		return err
	}
	defer db.Close()

	st := store.New(db)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := st.Migrate(ctx); err != nil {
		return err
	}
	if err := st.SetAdminPassword(ctx, args[0], string(password)); err != nil {
		return err
	}
	fmt.Println("admin password changed")
	return nil
}

func openDatabase(dsn string, logger *slog.Logger) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	var lastErr error
	for attempt := 1; attempt <= 30; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err = db.PingContext(ctx)
		cancel()
		if err == nil {
			return db, nil
		}
		lastErr = err
		logger.Warn("waiting for database", "attempt", attempt, "error", err)
		time.Sleep(2 * time.Second)
	}
	_ = db.Close()
	return nil, fmt.Errorf("database ping retries exhausted: %w", lastErr)
}
