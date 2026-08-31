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

	"github.com/emisell/api-payment-proxy/internal/config"
	"github.com/emisell/api-payment-proxy/internal/connectorrunner"
	"github.com/emisell/api-payment-proxy/internal/engine"
	"github.com/emisell/api-payment-proxy/internal/registry"
	"github.com/emisell/api-payment-proxy/internal/xendit"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(logger); err != nil {
		logger.Error("connector runner stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.LoadConnectorRunner()
	if err != nil {
		return err
	}
	xenditConnector, err := xendit.New(cfg.XenditBaseURL, cfg.ConnectorTimeout)
	if err != nil {
		return err
	}
	connectorRegistry, err := registry.New(xenditConnector)
	if err != nil {
		return err
	}
	runtime, err := engine.New(connectorRegistry)
	if err != nil {
		return err
	}
	handler, err := connectorrunner.New(runtime, cfg.Token, logger)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr: cfg.HTTPAddr, Handler: handler,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second,
		WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 16 << 10,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errorsChannel := make(chan error, 1)
	go func() {
		logger.Info("connector runner started", "address", server.Addr, "connectors", runtime.ConnectorCodes())
		errorsChannel <- server.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return server.Shutdown(shutdownContext)
	case err := <-errorsChannel:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
