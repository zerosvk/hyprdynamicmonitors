package dial

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type noopResponse struct{}

func (noopResponse) Validate() error { return nil }

func TestSyncQuerySocket_TimesOutOnHungServer(t *testing.T) {
	prev := queryTimeout
	queryTimeout = 100 * time.Millisecond
	defer func() { queryTimeout = prev }()

	socketPath := filepath.Join(t.TempDir(), "hypr.sock")
	lc := net.ListenConfig{}
	listener, err := lc.Listen(context.Background(), "unix", socketPath)
	require.NoError(t, err)
	defer listener.Close()

	// accept and never answer, like a hung compositor
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()

	conn, teardown, err := GetUnixSocketConnection(context.Background(), socketPath)
	require.NoError(t, err)
	defer teardown()

	done := make(chan error, 1)
	go func() {
		_, err := SyncQuerySocket[noopResponse](conn, "j/monitors all\n")
		done <- err
	}()

	select {
	case err := <-done:
		assert.Error(t, err, "a hung socket should time out")
	case <-time.After(2 * time.Second):
		t.Fatal("query did not time out")
	}

	select {
	case c := <-accepted:
		_ = c.Close()
	default:
	}
}
