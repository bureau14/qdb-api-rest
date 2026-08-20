// The qdb_rest binary: QuasarDB's HTTP front door. One HTTP listener
// serving the legacy compatibility endpoints and the /api/v2 API.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bureau14/qdb-api-rest/internal/httpapi"
)

// shutdownGrace bounds the drain of in-flight requests on SIGTERM; it stays
// below the 10 s the e2e harness allows between SIGTERM and SIGKILL.
const shutdownGrace = 8 * time.Second

// newServer assembles the HTTP server. No global write timeout: responses
// are streamed, so write deadlines are a per-request concern.
func newServer(listen string) *http.Server {
	return &http.Server{
		Addr:              listen,
		Handler:           httpapi.NewHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
}

// run serves until ctx is cancelled, then drains in-flight requests within
// shutdownGrace. A nil return means a clean shutdown.
func run(ctx context.Context, server *http.Server) error {
	errs := make(chan error, 1)
	go func() { errs <- server.ListenAndServe() }()
	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		drain, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		return server.Shutdown(drain)
	}
}

func main() {
	listen := flag.String("listen", ":40080", "HTTP listen address")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("listening", "addr", *listen)
	if err := run(ctx, newServer(*listen)); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
	slog.Info("shutdown complete")
}
