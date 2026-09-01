package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/emisell/api-payment-proxy/internal/config"
	"github.com/emisell/api-payment-proxy/internal/connectorrunner"
	"github.com/emisell/api-payment-proxy/internal/engine"
	"github.com/emisell/api-payment-proxy/internal/ipaymu"
	"github.com/emisell/api-payment-proxy/internal/registry"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(logger); err != nil {
		logger.Error("iPaymu Provider App stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.LoadIPaymuProviderApp()
	if err != nil {
		return err
	}
	providerConnector, err := ipaymu.New(cfg.SandboxBaseURL, cfg.LiveBaseURL, cfg.ConnectorTimeout)
	if err != nil {
		return err
	}
	digest, err := executableSHA256()
	if err != nil {
		return err
	}
	providerConnector.SetExecutableSHA256(digest)
	connectorRegistry, err := registry.New(providerConnector)
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
		logger.Info("iPaymu Provider App started", "address", server.Addr, "version", providerConnector.Manifest().Version, "executable_sha256", digest)
		errorsChannel <- listen(server, cfg.TLSCertPEM, cfg.TLSKeyPEM)
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

func listen(server *http.Server, certPEM, keyPEM []byte) error {
	if len(certPEM) == 0 {
		return server.ListenAndServe()
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return err
	}
	server.TLSConfig = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
	return server.ListenAndServeTLS("", "")
}

func executableSHA256() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err = io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
