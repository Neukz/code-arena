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

	"github.com/Neukz/code-arena/internal/api"
	"github.com/Neukz/code-arena/internal/config"
	"github.com/Neukz/code-arena/internal/db"
	"github.com/Neukz/code-arena/internal/queue"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"
)

// @title		CodeArena API
// @version		1.0.0
// @description	REST API for the CodeArena coding challenge platform.
// @BasePath	/

func main() {
	if err := run(); err != nil {
		slog.Error("api exited", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := newLogger(cfg.LogLevel)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	amqpConn, err := amqp.Dial(cfg.RabbitMQURL)
	if err != nil {
		return err
	}
	defer func() {
		if err := amqpConn.Close(); err != nil {
			log.Error("failed to close amqp connection", slog.String("error", err.Error()))
		}
	}()

	publisher, err := queue.NewPublisher(amqpConn)
	if err != nil {
		return err
	}
	defer func() {
		if err := publisher.Close(); err != nil {
			log.Error("failed to close publisher", slog.String("error", err.Error()))
		}
	}()

	handler := api.NewHandler(db.New(pool), publisher, log)
	srv := api.NewServer(":"+cfg.APIPort, handler.Routes())

	listenCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Info("starting api server", slog.String("addr", ":"+cfg.APIPort))
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-listenCtx.Done():
		log.Info("shutting down api server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error("server shutdown error", slog.String("error", err.Error()))
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func newLogger(level string) *slog.Logger {
	var opts *slog.HandlerOptions
	switch level {
	case "debug":
		opts = &slog.HandlerOptions{Level: slog.LevelDebug}
	case "warn":
		opts = &slog.HandlerOptions{Level: slog.LevelWarn}
	case "error":
		opts = &slog.HandlerOptions{Level: slog.LevelError}
	default:
		opts = &slog.HandlerOptions{Level: slog.LevelInfo}
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}
