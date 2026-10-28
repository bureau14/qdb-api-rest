// Package notify speaks systemd's notify protocol (sd_notify(3)): one
// datagram per state to the socket NOTIFY_SOCKET names. Outside systemd
// the variable is unset and every function does nothing. The binary
// sends READY=1 once every listener is bound and STOPPING=1 when the
// drain starts (cmd/qdb_rest/main.go).
package notify

import (
	"context"
	"log/slog"
	"net"
	"os"

	"github.com/bureau14/qdb-api-rest/internal/observe"
)

// The states the binary sends. Each is one assignment line of the
// protocol, and Send adds the newline.
const (
	Ready    = "READY=1"
	Stopping = "STOPPING=1"
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
