// Package notify speaks systemd's notify protocol (sd_notify(3)): one
// datagram per state to the socket NOTIFY_SOCKET names, and the watchdog
// keepalive at half of WATCHDOG_USEC. Unset variables mean no systemd,
// and every function does nothing. main sends READY=1 once every
// listener is bound, STOPPING=1 when the drain starts, and WATCHDOG=1
// between the two.
package notify

import (
	"context"
	"log/slog"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/bureau14/qdb-api-rest/internal/observe"
)

// The states the binary sends, one assignment line each.
const (
	Ready    = "READY=1"
	Stopping = "STOPPING=1"
	Watchdog = "WATCHDOG=1"
)

// warn logs a failed send. Failing the start instead was rejected,
// because a misconfigured socket is systemd's to report, and silence was
// rejected, because the operator would then read only systemd's timeout.
func warn(ctx context.Context, path string, err error) {
	observe.Logger(ctx).WarnContext(ctx, "systemd notify failed", slog.String("socket", path), observe.Err(err))
}

// Send writes one state line to the socket NOTIFY_SOCKET names and does
// nothing when the variable is unset. A failed send is one warning and
// no error, because serving does not depend on systemd.
func Send(ctx context.Context, state string) {
	path := os.Getenv("NOTIFY_SOCKET")
	if path == "" {
		return
	}
	// The path goes through as given: Go rewrites a leading @ to the
	// abstract namespace's NUL on Linux.
	conn, err := net.Dial("unixgram", path)
	if err != nil {
		warn(ctx, path, err)
		return
	}
	defer func() { _ = conn.Close() }()
	// One write is one datagram.
	if _, err := conn.Write([]byte(state + "\n")); err != nil {
		warn(ctx, path, err)
	}
}

// watchdogInterval answers WATCHDOG_USEC as a duration, and zero when
// there is no watchdog for this process: the variable unset, unparsable
// or zero, or WATCHDOG_PID naming another process (sd_watchdog_enabled(3)).
func watchdogInterval() time.Duration {
	usec, err := strconv.ParseUint(os.Getenv("WATCHDOG_USEC"), 10, 64)
	if err != nil || usec == 0 {
		return 0
	}
	if pid := os.Getenv("WATCHDOG_PID"); pid != "" && pid != strconv.Itoa(os.Getpid()) {
		return 0
	}
	return time.Duration(usec) * time.Microsecond
}

// RunWatchdog sends WATCHDOG=1 every half of WATCHDOG_USEC until ctx
// ends, and returns at once when there is no watchdog for this process.
// It blocks. A tick proves that this process schedules goroutines and
// nothing about the cluster.
func RunWatchdog(ctx context.Context) {
	interval := watchdogInterval()
	if interval == 0 {
		return
	}
	// Half the interval is systemd's recommendation (sd_watchdog_enabled(3)).
	ticker := time.NewTicker(interval / 2)
	defer ticker.Stop()
	// A probe of the cluster per tick was rejected, because a cluster
	// outage must not kill the gateway. ctx is the process context, which
	// ends before STOPPING=1 goes out, so the loop ends when the drain
	// starts and a failed tick only warns.
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			Send(ctx, Watchdog)
		}
	}
}
