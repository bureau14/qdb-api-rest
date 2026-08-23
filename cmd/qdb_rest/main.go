// The qdb_rest binary: QuasarDB's HTTP front door. Serves the legacy
// compatibility endpoints and the /api/v2 API on an HTTP and an HTTPS
// listener, both configured by internal/config.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bureau14/qdb-api-rest/internal/config"
	"github.com/bureau14/qdb-api-rest/internal/httpapi"
	"github.com/bureau14/qdb-api-rest/internal/observe"
	"github.com/bureau14/qdb-api-rest/internal/tlsconf"
)

// shutdownGrace bounds the drain of in-flight requests on SIGTERM; it stays
// below the 10s the e2e harness allows between SIGTERM and SIGKILL.
const shutdownGrace = 8 * time.Second

// newServer assembles one listener's server; the HTTPS listener carries a
// tls.Config, plain HTTP passes nil. BaseContext hands the process
// context (and so the logger) to every request; ErrorLog routes net/http's
// own complaints through the same handler. No global write timeout:
// responses are streamed, so write deadlines are a per-request concern.
func newServer(ctx context.Context, addr string, handler http.Handler, tlsConfig *tls.Config) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ErrorLog:          slog.NewLogLogger(observe.Logger(ctx).Handler(), slog.LevelError),
	}
}

// newServers builds the enabled listeners from config and logs what each
// one serves.
func newServers(ctx context.Context, cfg config.Config, handler http.Handler) ([]*http.Server, error) {
	log := observe.Logger(ctx)
	var servers []*http.Server
	if cfg.Listen.HTTP != "" {
		servers = append(servers, newServer(ctx, cfg.Listen.HTTP, handler, nil))
		log.InfoContext(ctx, "listening", "proto", "http", "addr", cfg.Listen.HTTP)
	}
	if cfg.Listen.HTTPS != "" {
		tlsConfig, info, err := tlsconf.Load(cfg.TLS, time.Now())
		if err != nil {
			return nil, err
		}
		if info.Source == tlsconf.SourceEphemeral {
			log.WarnContext(ctx, "no tls certificate configured; generated an ephemeral self-signed certificate",
				"fingerprint_sha256", info.Fingerprint,
				"hint", "the identity changes on every start; set tls.certificate and tls.private_key for a stable one")
		}
		servers = append(servers, newServer(ctx, cfg.Listen.HTTPS, handler, tlsConfig))
		log.InfoContext(ctx, "listening", "proto", "https", "addr", cfg.Listen.HTTPS,
			"certificate", string(info.Source), "not_after", info.NotAfter)
	}
	return servers, nil
}

// serve runs one server to its terminal error.
func serve(server *http.Server) error {
	if server.TLSConfig != nil {
		return server.ListenAndServeTLS("", "")
	}
	return server.ListenAndServe()
}

// run serves all listeners until ctx is cancelled or one fails, then
// drains in-flight requests within shutdownGrace. A nil return means a
// clean shutdown.
func run(ctx context.Context, servers []*http.Server) error {
	errs := make(chan error, len(servers))
	for _, server := range servers {
		go func() { errs <- serve(server) }()
	}
	var failure error
	select {
	case failure = <-errs:
	case <-ctx.Done():
	}
	drain, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	for _, server := range servers {
		if err := server.Shutdown(drain); err != nil && failure == nil {
			failure = err
		}
	}
	if errors.Is(failure, http.ErrServerClosed) {
		return nil
	}
	return failure
}

func main() {
	cfg, err := config.Load("qdb_rest", os.Args[1:], os.LookupEnv, os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		os.Exit(0)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	logger, err := observe.NewLogger(cfg.Log, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, err) // unreachable while config validates the vocabulary
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(observe.WithLogger(context.Background(), logger), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	servers, err := newServers(ctx, cfg, httpapi.NewHandler())
	if err != nil {
		logger.ErrorContext(ctx, "startup failed", observe.Err(err))
		os.Exit(1)
	}

	if err := run(ctx, servers); err != nil {
		logger.ErrorContext(ctx, "server failed", observe.Err(err))
		os.Exit(1)
	}
	logger.InfoContext(ctx, "shutdown complete")
}
