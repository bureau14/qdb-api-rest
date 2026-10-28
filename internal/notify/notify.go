// Package notify speaks systemd's notify protocol (sd_notify(3)): one
// datagram per state to the socket NOTIFY_SOCKET names, and the watchdog
// keepalive at half of WATCHDOG_USEC. Outside systemd the variables are
// unset and every function does nothing. The binary sends READY=1 once
// every listener is bound, STOPPING=1 when the drain starts, and
// WATCHDOG=1 between the two (cmd/qdb_rest/main.go).
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

// The states the binary sends. Each is one assignment line of the
// protocol, and Send adds the newline.
const (
	Ready    = "READY=1"
	Stopping = "STOPPING=1"
	Watchdog = "WATCHDOG=1"
)

// warn logs the one line a failed send leaves, with the socket path and
// the error. Failing the start on a failed READY=1 was rejected, because
// a misconfigured socket is systemd's to report and the serving path
// owes it nothing. Silence was rejected, because the operator would then
// read only systemd's timeout.
func warn(ctx context.Context, path string, err error) {
	observe.Logger(ctx).WarnContext(ctx, "systemd notify failed", slog.String("socket", path), observe.Err(err))
}

// Send writes one state line to the socket NOTIFY_SOCKET names and does
// nothing when the variable is unset. A failed send logs one warning on
// the context's logger and is not an error to the caller, because the
// serving path does not depend on systemd and a bad socket surfaces at
// systemd's own start timeout.
func Send(ctx context.Context, state string) {
	// The steps:
	//
	//  1. Return when the variable is unset.
	//  2. Dial the socket as a connected datagram socket.
	//  3. Write the line and close.
	//
	// Any failure of steps 2 and 3 goes through warn and ends the call.
	//
	// 1. Unset means not under systemd, and nothing is sent. The empty
	//    string is the same case.
	path := os.Getenv("NOTIFY_SOCKET")
	if path == "" {
		return
	}
	// 2. Dial a connected datagram socket on the path as given. Go
	//    rewrites a leading @ to the abstract namespace's NUL on Linux,
	//    so the value needs no edit.
	conn, err := net.Dial("unixgram", path)
	if err != nil {
		warn(ctx, path, err)
		return
	}
	defer func() { _ = conn.Close() }()
	// 3. Write the line with its newline. One write is one datagram.
	if _, err := conn.Write([]byte(state + "\n")); err != nil {
		warn(ctx, path, err)
	}
}

// watchdogInterval answers the interval WATCHDOG_USEC names, and zero
// when there is no watchdog for this process: the variable unset,
// unparsable or zero, or WATCHDOG_PID naming another process, which
// means the variables were set for an ancestor (sd_watchdog_enabled(3)).
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

// RunWatchdog sends WATCHDOG=1 every half of the interval WATCHDOG_USEC
// names until ctx ends, and returns at once when the variable is unset
// or WATCHDOG_PID names another process. It blocks, so the caller runs
// it on a goroutine. Each tick proves that this process schedules
// goroutines and nothing about the cluster.
func RunWatchdog(ctx context.Context) {
	// The steps:
	//
	//  1. Return when there is no watchdog for this process.
	//  2. Tick at half the interval.
	//  3. Send on each tick until ctx ends.
	//
	// 1. No watchdog means the function returns; watchdogInterval says
	//    which cases those are.
	interval := watchdogInterval()
	if interval == 0 {
		return
	}
	// 2. Half the interval is systemd's own recommendation
	//    (sd_watchdog_enabled(3)), not a measurement.
	ticker := time.NewTicker(interval / 2)
	defer ticker.Stop()
	// 3. Send on each tick through Send, so a failed tick warns and the
	//    loop goes on. A probe of the cluster or of the HTTP listener per
	//    tick was rejected, because a cluster outage must not kill the
	//    gateway and the keepalive is tied to the serving loop alone. The
	//    context is the process context, which ends before STOPPING=1 is
	//    sent, so the loop ends when the drain starts.
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			Send(ctx, Watchdog)
		}
	}
}
