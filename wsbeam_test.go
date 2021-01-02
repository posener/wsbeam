package wsbeam

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBeam(t *testing.T) {
	t.Parallel()

	b := New(OptLogger(t.Logf))
	s := newServer(t, b)
	c := connect(t, s)

	err := b.Send("test")
	require.NoError(t, err)

	var result string
	err = c.ReadJSON(&result)
	require.NoError(t, err)
	assert.Equal(t, "test", result)
}

func TestBeamDisconnectedClientAreRemoved(t *testing.T) {
	t.Parallel()

	b := New(OptLogger(t.Logf))
	s := newServer(t, b)

	// Create a connection.
	c := connect(t, s)

	// Check pears size.
	b.lock.Lock()
	assert.Equal(t, 1, len(b.pears))
	b.lock.Unlock()

	// Close the connection.
	c.Close()

	// Give server time to clear the connection.
	time.Sleep(time.Second)

	// Check that the connection was deleted from the server.
	b.lock.Lock()
	assert.Equal(t, 0, len(b.pears))
	b.lock.Unlock()
}

// Tests send to many connected clients.
func TestBeamMultiConnections(t *testing.T) {
	t.Parallel()
	const count = 100

	b := New(OptLogger(t.Logf))
	s := newServer(t, b)

	var conns []*websocket.Conn

	for i := 0; i < count; i++ {
		c := connect(t, s)
		conns = append(conns, c)
	}

	err := b.Send("test")
	require.NoError(t, err)

	for _, c := range conns {
		var result string
		err = c.ReadJSON(&result)
		require.NoError(t, err)
		assert.Equal(t, "test", result)
	}
}

func TestBeamNoLog(t *testing.T) {
	t.Parallel()

	b := New(OptLogger(nil))
	s := newServer(t, b)
	c := connect(t, s)

	err := b.Send("test")
	require.NoError(t, err)

	var result string
	err = c.ReadJSON(&result)
	require.NoError(t, err)
	assert.Equal(t, "test", result)
}

func newServer(t *testing.T, b *Beam) *httptest.Server {
	s := httptest.NewServer(b)
	s.URL = strings.Replace(s.URL, "http", "ws", 1)
	t.Cleanup(func() { s.Close() })
	return s
}

func connect(t *testing.T, s *httptest.Server) *websocket.Conn {
	c, resp, err := websocket.DefaultDialer.Dial(s.URL, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	return c
}
