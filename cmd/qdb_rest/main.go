// The qdb_rest binary is QuasarDB's HTTP front door. It serves the routes
// internal/httpapi assembles on an HTTP and an HTTPS listener, and the
// Flight SQL server of internal/flightsql on a third, all configured by
// internal/config.
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
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bureau14/qdb-api-rest/internal/auth"
	"github.com/bureau14/qdb-api-rest/internal/build"
	"github.com/bureau14/qdb-api-rest/internal/config"
	"github.com/bureau14/qdb-api-rest/internal/flightsql"
	"github.com/bureau14/qdb-api-rest/internal/httpapi"
	"github.com/bureau14/qdb-api-rest/internal/notify"
	"github.com/bureau14/qdb-api-rest/internal/observe"
	"github.com/bureau14/qdb-api-rest/internal/olap"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
	"github.com/bureau14/qdb-api-rest/internal/tlsconf"
)

// versionText renders the version block that all QuasarDB binaries share,
// plus the linked C API and DuckDB versions. Those two lines are not
// compile-time information. The CI smoke run of --version proves both
// links on every platform.
func versionText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "quasardb rest api version: %s\n", build.Version)
	fmt.Fprintf(&b, "build: %s\n", build.Commit)
	fmt.Fprintf(&b, "date: %s\n\n", build.BuildTime)
	fmt.Fprintf(&b, "target: %s-%s\n", runtime.GOARCH, runtime.GOOS)
	fmt.Fprintf(&b, "compiler: %s\n", runtime.Version())
	fmt.Fprintf(&b, "c api: %s (%s)\n", qdb.APIVersion(), qdb.APIBuild())
	fmt.Fprintf(&b, "duckdb: %s\n", olap.Version())
	if runtime.GOARCH == "amd64" && build.GOAMD64 != "" {
		fmt.Fprintf(&b, "arch level: %s\n", build.GOAMD64)
	}
	fmt.Fprintf(&b, "\nbuild type: %s\n\n", build.BuildMode)
	b.WriteString("Copyright (c) 2009-2026, quasardb SAS. All rights reserved.\n")
	return b.String()
}

// listener is one of the binary's servers as the serve loop and shutdown
// see it. Serve answers nil after a clean stop, and Shutdown drains under
// the caller's deadline. The HTTP and Flight servers share this shape,
// so the serve loop and the drain are one loop each. A Flight branch
// next to a slice of HTTP servers would need two loops kept in step.
type listener interface {
	Serve() error
	Shutdown(context.Context)
}

// httpListener adapts *http.Server to listener over a listener bound
// before serving. Serve treats ErrServerClosed as a clean stop, and
// Shutdown closes the listener through http.Server.Shutdown.
type httpListener struct {
	*http.Server
	ln net.Listener
}

func (l httpListener) Serve() error {
	var err error
	if l.TLSConfig != nil {
		// ServeTLS with empty file names serves TLSConfig.Certificates,
		// which tlsconf.Load filled.
		err = l.ServeTLS(l.ln, "", "")
	} else {
		err = l.Server.Serve(l.ln)
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (l httpListener) Shutdown(ctx context.Context) { _ = l.Server.Shutdown(ctx) }

// newHTTPServer binds addr and assembles one HTTP listener's server over
// it, so a port in use fails the start before serving, as it does for
// Flight. tlsConfig is nil for plain HTTP. ctx is the request base
// context of newListeners, step 1.
func newHTTPServer(ctx context.Context, addr string, handler http.Handler, tlsConfig *tls.Config) (httpListener, error) {
	// The bind is here, so that main sends READY=1 only once every port
	// is held.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return httpListener{}, err
	}
	// No WriteTimeout: responses are streamed, so write deadlines are a
	// per-request concern. Every request inherits ctx, and net/http's own
	// complaints go through the process logger.
	return httpListener{&http.Server{
		Addr:              addr,
		Handler:           handler,
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ErrorLog:          slog.NewLogLogger(observe.Logger(ctx).Handler(), slog.LevelError),
	}, ln}, nil
}

// newListeners builds the listeners from config and logs what each one
// serves. HTTP is off unless configured, and the other two always start
// (ADR-0001). Every listener is bound when it returns, so a port in use
// fails the start.
func newListeners(ctx context.Context, cfg config.Config, handler http.Handler) ([]listener, error) {
	// The steps:
	//
	//  1. Derive the context every HTTP request inherits.
	//  2. Load the TLS material once.
	//  3. Bind and add HTTP, when configured, with one warning when the
	//     cluster is secure.
	//  4. Bind and add HTTPS.
	//  5. Bind and add Flight.
	log := observe.Logger(ctx)
	var listeners []listener
	// 1. Requests inherit the process values but not the cancellation:
	//    the signal cuts no stream, the grace ends it.
	base := context.WithoutCancel(ctx)
	// 2. HTTPS and Flight serve the same certificate, so the TLS material
	//    loads once. Loading it once keeps an ephemeral certificate to one
	//    identity per start and logs its warning once.
	tlsConfig, info, err := tlsconf.Load(cfg.TLS, time.Now())
	if err != nil {
		return nil, err
	}
	if info.Source == tlsconf.SourceEphemeral {
		log.WarnContext(ctx, "no tls certificate configured; generated an ephemeral self-signed certificate",
			"fingerprint_sha256", info.Fingerprint,
			"hint", "the identity changes on every start; set tls.certificate and tls.private_key for a stable one")
	}
	// 3. HTTP, when configured, bound and over base, with one warning
	//    when the cluster is secure, because access tokens then travel in
	//    plain text. No key silences it, and a deployment inside a VPC is
	//    the one case that accepts it. Secured is the binary's one reading
	//    of "has a public key". tests/e2e/round-trip.sh
	//    (PLAINTEXT_WARNING) greps the message, so the two change
	//    together. The line is here, because this step already decides
	//    that the HTTP listener exists. A bind error returns, as the TLS
	//    error of step 2 does.
	if cfg.Listen.HTTP != "" {
		h, err := newHTTPServer(base, cfg.Listen.HTTP, handler, nil)
		if err != nil {
			return nil, err
		}
		listeners = append(listeners, h)
		log.InfoContext(ctx, "listening", "proto", "http", "addr", cfg.Listen.HTTP)
		if qdb.ClusterFrom(ctx).Secured() {
			log.WarnContext(ctx, "http listener on a secure cluster: access tokens travel in plain text",
				"addr", cfg.Listen.HTTP,
				"hint", "serve the HTTPS listener only, unless the network between clients and this server is already encrypted")
		}
	}
	// 4. HTTPS, bound and over base. A bind error returns.
	hs, err := newHTTPServer(base, cfg.Listen.HTTPS, handler, tlsConfig)
	if err != nil {
		return nil, err
	}
	listeners = append(listeners, hs)
	log.InfoContext(ctx, "listening", "proto", "https", "addr", cfg.Listen.HTTPS,
		"certificate", string(info.Source), "not_after", info.NotAfter)
	// 5. Flight, bound. It takes ctx and not base, because its
	//    interceptors splice the process values onto each call's own
	//    context and it has no readiness route. flightsql.New binds the
	//    port, so a port in use fails the start, as bad TLS material does.
	fs, err := flightsql.New(ctx, cfg.Listen.Flight, tlsConfig)
	if err != nil {
		return nil, err
	}
	listeners = append(listeners, fs)
	log.InfoContext(ctx, "listening", "proto", "flight", "addr", fs.Addr().String(),
		"certificate", string(info.Source), "not_after", info.NotAfter)
	return listeners, nil
}

// serveUntilDone serves all listeners until the context is cancelled by
// SIGINT or SIGTERM or one listener fails. It returns the first terminal
// error, or nil on a clean cancel. It shuts nothing down, because
// shutdown does that under the configured grace.
func serveUntilDone(ctx context.Context, listeners []listener) error {
	errs := make(chan error, len(listeners))
	for _, l := range listeners {
		go func() { errs <- l.Serve() }()
	}
	select {
	case failure := <-errs:
		return failure
	case <-ctx.Done():
		return nil
	}
}

// shutdown drains the listeners first and the cluster second, both under
// the grace of listen.shutdown_grace. The listeners drain in parallel.
// The Flight server drains next to the HTTP servers and is not stopped
// hard, because the graceful drain covers Flight streams (docs/brief.md,
// Resilience). The order matters, because in-flight calls on either plane
// hold sessions. Once every listener has drained, the cluster has no
// callers left, and its close waits only for idle sessions.
func shutdown(ctx context.Context, grace time.Duration, listeners []listener, cluster *qdb.Cluster) {
	// The deadline is the caller's, read from the configuration once in
	// main, so this function has no constant of its own. A package
	// variable was rejected, because the binary keeps no package-level
	// state (internal/AGENTS.md, Code).
	drain, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	var wg sync.WaitGroup
	for _, l := range listeners {
		wg.Go(func() { l.Shutdown(drain) })
	}
	wg.Wait()
	if err := cluster.Close(drain); err != nil {
		observe.Logger(ctx).WarnContext(ctx, "cluster did not drain within the shutdown grace", observe.Err(err))
	}
}

func main() {
	// 1. Load the configuration from the defaults, the file, the
	//    environment and the flags. --version and --help are answered here
	//    and exit. A config error refuses the start with exit code 2, the
	//    only startup failure that does.
	cfg, err := config.Load("qdb_rest", os.Args[1:], os.LookupEnv, os.Stderr)
	if errors.Is(err, config.ErrVersionRequested) {
		fmt.Print(versionText())
		os.Exit(0)
	}
	if errors.Is(err, flag.ErrHelp) {
		os.Exit(0)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	// 2. Build the process logger and place it in the root context. The
	//    root context also ends on SIGINT or SIGTERM, which stops serving
	//    below.
	logger, err := observe.NewLogger(cfg.Log, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, err) // unreachable while config validates the vocabulary
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(observe.WithLogger(context.Background(), logger), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger.InfoContext(ctx, "starting", "version", build.Version, "commit", build.Commit, "build_mode", build.BuildMode,
		"qdb_api_version", qdb.APIVersion())

	// 3. Bind the cluster. The binding's own logger is routed into the
	//    process logger before anything can dial. The cluster dials
	//    nothing at startup, and it travels in the context so handlers
	//    reach it there.
	qdb.InstallLogger(logger)
	cluster := qdb.New(cfg, time.Now)
	ctx = qdb.WithCluster(ctx, cluster)

	// 4. Derive the token keychain once from the configured passphrases.
	//    It travels in the context like the cluster. Derivation pays the
	//    argon2id cost per passphrase here, at startup, and never per
	//    request.
	tokens, err := auth.New(ctx, cfg.Auth, time.Now)
	if err != nil {
		logger.ErrorContext(ctx, "startup failed", observe.Err(err))
		os.Exit(1)
	}
	ctx = auth.WithTokens(ctx, tokens)

	// 5. Build the listeners over the root context, so every request
	//    inherits the logger, the cluster and the keychain. Loading the
	//    TLS material and binding any of the three ports can fail here.
	//    Then tell systemd the service is up, because every port is held,
	//    and start the keepalive on the root context, so it ends with the
	//    serve loop and never ticks into the drain, where a tick would
	//    re-arm the watchdog systemd disarmed on STOPPING=1.
	listeners, err := newListeners(ctx, cfg, httpapi.NewHandler(cfg.Metrics, cfg.Debug))
	if err != nil {
		logger.ErrorContext(ctx, "startup failed", observe.Err(err))
		os.Exit(1)
	}
	notify.Send(ctx, notify.Ready)
	go notify.RunWatchdog(ctx)

	// 6. Serve until a signal or a listener failure. Then end the process
	//    context, so the keepalive stops on the listener-failure path
	//    too, where no signal ended it; then tell systemd the stop has
	//    begun, after the context's end and never before it, so no
	//    keepalive is planned after STOPPING=1; then drain the listeners
	//    first and the cluster second under listen.shutdown_grace. The
	//    listeners close their ports as the drain's first act, so a load
	//    balancer's next connection is refused while in-flight streams
	//    finish. A tick already in flight at the context's
	//    end may still land after STOPPING=1 and re-arm the watchdog,
	//    which the exit outruns. stop is idempotent, so the deferred call
	//    stays.
	runErr := serveUntilDone(ctx, listeners)
	stop()
	notify.Send(ctx, notify.Stopping)
	shutdown(ctx, cfg.Listen.ShutdownGrace, listeners, cluster)
	if runErr != nil {
		logger.ErrorContext(ctx, "server failed", observe.Err(runErr))
		os.Exit(1)
	}
	logger.InfoContext(ctx, "shutdown complete")
}
