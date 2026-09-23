package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Loopayeh/pkg-sender/nas/internal/discovery"
	"github.com/Loopayeh/pkg-sender/nas/internal/httpserver"
	"github.com/Loopayeh/pkg-sender/nas/internal/pkgstore"
	"github.com/Loopayeh/pkg-sender/nas/internal/ps5"
)

func main() {
	logger := log.New(os.Stdout, "pkg-sender-nas ", log.LstdFlags|log.Lmsgprefix)

	cfg, err := loadConfig()
	if err != nil {
		logger.Fatalf("configuration error: %v", err)
	}

	store, err := pkgstore.New(cfg.packageDir)
	if err != nil {
		logger.Fatalf("package store: %v", err)
	}
	count, err := store.Scan()
	if err != nil {
		logger.Fatalf("initial package scan: %v", err)
	}

	ps5Client, err := ps5.New(cfg.ps5IP, cfg.ps5Port, 10*time.Second)
	if err != nil {
		logger.Fatalf("PS5 client: %v", err)
	}

	ps5Discovery := discovery.New(cfg.ps5IP)
	app, err := httpserver.New(store, ps5Client, cfg.publicBaseURL, logger, ps5Discovery)
	if err != nil {
		logger.Fatalf("HTTP server: %v", err)
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		if err := ps5Discovery.Listen(sigCtx); err != nil && sigCtx.Err() == nil {
			logger.Printf("PS5 discovery unavailable: %v", err)
		}
	}()

	httpSrv := &http.Server{
		Addr:              cfg.listen,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		// No WriteTimeout: PKG transfers can legitimately run for a long time.
	}

	logger.Printf("package root: %s", store.Root())
	logger.Printf("initial scan: %d pkg file(s)", count)
	logger.Printf("PS5 receiver: http://%s:%d", cfg.ps5IP, cfg.ps5Port)
	logger.Printf("PS5 discovery: UDP :%d (best effort)", discovery.BeaconPort)
	logger.Printf("public package URL base: %s", cfg.publicBaseURL)
	logger.Printf("listening on %s", cfg.listen)

	errCh := make(chan error, 1)
	go func() {
		errCh <- httpSrv.ListenAndServe()
	}()

	select {
	case <-sigCtx.Done():
		logger.Printf("shutdown requested")
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Fatalf("HTTP server stopped: %v", err)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		logger.Printf("graceful shutdown failed: %v", err)
		_ = httpSrv.Close()
	}
}

type config struct {
	packageDir    string
	listen        string
	publicBaseURL string
	ps5IP         string
	ps5Port       int
}

func loadConfig() (config, error) {
	cfg := config{
		packageDir:    envOr("PKGSENDER_PACKAGE_DIR", "/packages"),
		listen:        envOr("PKGSENDER_LISTEN", ":9898"),
		publicBaseURL: strings.TrimSpace(os.Getenv("PKGSENDER_PUBLIC_BASE_URL")),
		ps5IP:         strings.TrimSpace(os.Getenv("PKGSENDER_PS5_IP")),
		ps5Port:       12800,
	}
	if cfg.ps5IP == "" {
		return config{}, errors.New("PKGSENDER_PS5_IP is required")
	}
	if cfg.publicBaseURL == "" {
		return config{}, errors.New("PKGSENDER_PUBLIC_BASE_URL is required")
	}

	if raw := strings.TrimSpace(os.Getenv("PKGSENDER_PS5_PORT")); raw != "" {
		port, err := strconv.Atoi(raw)
		if err != nil || port < 1 || port > 65535 {
			return config{}, fmt.Errorf("invalid PKGSENDER_PS5_PORT %q", raw)
		}
		cfg.ps5Port = port
	}
	return cfg, nil
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
