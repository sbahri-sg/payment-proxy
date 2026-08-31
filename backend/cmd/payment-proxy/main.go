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

	"github.com/emisell/api-payment-proxy/internal/api"
	"github.com/emisell/api-payment-proxy/internal/config"
	"github.com/emisell/api-payment-proxy/internal/connector"
	"github.com/emisell/api-payment-proxy/internal/database"
	"github.com/emisell/api-payment-proxy/internal/emisellreceiver"
	"github.com/emisell/api-payment-proxy/internal/engine"
	"github.com/emisell/api-payment-proxy/internal/outbox"
	"github.com/emisell/api-payment-proxy/internal/registry"
	"github.com/emisell/api-payment-proxy/internal/remoteconnector"
	"github.com/emisell/api-payment-proxy/internal/secrets"
	"github.com/emisell/api-payment-proxy/internal/servicekeys"
	"github.com/emisell/api-payment-proxy/internal/store"
	"github.com/emisell/api-payment-proxy/internal/webhooksettings"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	if err := run(logger); err != nil {
		logger.Error("payment proxy stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	mode := "api"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	poolConfig.MaxConns = int32(cfg.DatabaseMaxConns)
	poolConfig.MinConns = int32(cfg.DatabaseMinConns)
	poolConfig.MaxConnLifetime = cfg.DatabaseMaxLifetime
	poolConfig.MaxConnIdleTime = cfg.DatabaseMaxIdleTime
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return err
	}

	switch mode {
	case "migrate":
		return database.Migrate(ctx, pool)
	case "worker":
		cipher, cipherErr := secrets.New(cfg.CredentialKey)
		if cipherErr != nil {
			return cipherErr
		}
		webhookSettings := webhooksettings.NewService(
			webhooksettings.NewPostgresRepository(pool), cipher, cfg.AppEnv,
			cfg.WebhookAllowHTTP, cfg.WebhookAllowPrivate, cfg.WebhookTimeout,
			webhooksettings.Fallback{
				Enabled:     cfg.EmisellWebhookURL != "" && cfg.EmisellWebhookSecret != "",
				CallbackURL: cfg.EmisellWebhookURL, Secret: cfg.EmisellWebhookSecret,
			},
		)
		worker, err := outbox.New(cfg, store.New(pool), webhookSettings, logger)
		if err != nil {
			return err
		}
		return worker.Run(ctx)
	case "emisell-receiver":
		handler, err := emisellreceiver.New(cfg.EmisellReceiverSecret, cfg.EmisellReceiverSkew, store.New(pool), logger)
		if err != nil {
			return err
		}
		return serve(ctx, &http.Server{
			Addr: cfg.EmisellReceiverAddr, Handler: handler,
			ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
			WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
			MaxHeaderBytes: 16 << 10,
		}, logger, "Emisell webhook receiver")
	case "api":
		if err := cfg.ValidateRuntime(); err != nil {
			return err
		}
	default:
		return errors.New("usage: payment-proxy [api|worker|migrate|emisell-receiver]")
	}

	cipher, err := secrets.New(cfg.CredentialKey)
	if err != nil {
		return err
	}
	connectors := make([]connector.Connector, 0, len(cfg.ConnectorRunners))
	for _, connectorRuntime := range cfg.ConnectorRunners {
		discovered, discoverErr := remoteconnector.DiscoverWithCAPEM(ctx, connectorRuntime.BaseURL, connectorRuntime.Token, cfg.ConnectorTimeout, cfg.ConnectorTLSCAPEM)
		if discoverErr != nil {
			return discoverErr
		}
		connectors = append(connectors, discovered...)
	}
	connectorRegistry, err := registry.New(connectors...)
	if err != nil {
		return err
	}
	paymentEngine, err := engine.New(connectorRegistry)
	if err != nil {
		return err
	}
	database := store.New(pool)
	serviceKeyService := servicekeys.NewService(servicekeys.NewPostgresRepository(pool))
	webhookSettings := webhooksettings.NewService(
		webhooksettings.NewPostgresRepository(pool), cipher, cfg.AppEnv,
		cfg.WebhookAllowHTTP, cfg.WebhookAllowPrivate, cfg.WebhookTimeout,
		webhooksettings.Fallback{
			Enabled:     cfg.EmisellWebhookURL != "" && cfg.EmisellWebhookSecret != "",
			CallbackURL: cfg.EmisellWebhookURL, Secret: cfg.EmisellWebhookSecret,
		},
	)
	handler := api.New(cfg, database, paymentEngine, cipher, serviceKeyService, webhookSettings, logger)
	server := &http.Server{
		Addr: cfg.HTTPAddr, Handler: handler,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 32 << 10,
	}
	return serve(ctx, server, logger, "payment proxy API")
}

func serve(ctx context.Context, server *http.Server, logger *slog.Logger, service string) error {
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info(service+" started", "address", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
