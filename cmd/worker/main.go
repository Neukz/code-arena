package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Neukz/code-arena/internal/config"
	"github.com/Neukz/code-arena/internal/db"
	"github.com/Neukz/code-arena/internal/queue"
	"github.com/Neukz/code-arena/internal/sandbox"
	"github.com/Neukz/code-arena/internal/worker"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moby/moby/client"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	consumerPrefetch   = 1
	consumerMaxRetries = 3
	shutdownTimeout    = 15 * time.Second
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker exited", slog.String("error", err.Error()))
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

	dockerClient, err := client.New(client.FromEnv)
	if err != nil {
		return err
	}
	if _, err := dockerClient.Ping(ctx, client.PingOptions{}); err != nil {
		return err
	}

	queries := db.New(pool)
	runner := sandbox.NewRunner(dockerClient)
	processor := worker.NewProcessor(queries, runner, log)
	handler := worker.Recover(processor.Process, queries, log)

	consumer, err := queue.NewConsumer(amqpConn, handler, log, consumerPrefetch, consumerMaxRetries)
	if err != nil {
		return err
	}
	defer func() {
		if err := consumer.Close(); err != nil {
			log.Error("failed to close consumer", slog.String("error", err.Error()))
		}
	}()

	listenCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Info("starting worker", slog.String("rabbitmq_url", cfg.RabbitMQURL))
		errCh <- consumer.Start(listenCtx)
	}()

	select {
	case <-listenCtx.Done():
		log.Info("shutting down worker")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		// Wait for the consumer loop to finish the in-flight message after cancellation.
		select {
		case err := <-errCh:
			if err != nil && !errors.Is(err, context.Canceled) {
				log.Error("consumer stopped with error", slog.String("error", err.Error()))
			}
		case <-shutdownCtx.Done():
			log.Error("shutdown timed out waiting for consumer to finish")
		}
		return nil
	case err := <-errCh:
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
