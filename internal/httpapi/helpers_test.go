package httpapi

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/bureau14/qdb-api-rest/internal/observe"
)

// requireQdbd fails fast with the recipe when the insecure cluster is down.
func requireQdbd(t *testing.T) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", "127.0.0.1:2836", time.Second)
	if err != nil {
		t.Fatalf("qdbd not answering on 127.0.0.1:2836; run: bash scripts/tests/setup/start-services.sh")
	}
	_ = conn.Close()
}

// withLoggerForTest returns a background context carrying log.
func withLoggerForTest(log *slog.Logger) context.Context {
	return observe.WithLogger(context.Background(), log)
}
