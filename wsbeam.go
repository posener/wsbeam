// Package wsbeam provides a WebSocket HTTP handler that can be used to beam (broadcast) data to all
// connections.
package wsbeam

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

// Beam is an HTTP handler that can send data to all connected connections.
type Beam struct {
	// pears stores all the connected pears. It is protected for concurrent access by the lock
	// field.
	pears map[*pear]bool
	lock  sync.Mutex

	// buffer is the number of messages, per connection, that the server stores when client does not
	// read them, without discarding new messages.
	buffer int

	// upgrader is the websocket upgarder.
	upgrader websocket.Upgrader

	// headers that the server returns for connected clients.
	headers http.Header

	// logger is the logging function. if nil, no log will be written.
	logger func(string, ...interface{})
}

// New returns a new Beam with the given options. This beam should be mounted as an HTTP handler.
// Clients can connect with websocket connection to this handler. All data that is sent to the
// `Send` method will be sent to all connected connections.
func New(ops ...func(*Beam)) *Beam {
	// Default values:
	b := &Beam{
		pears:  map[*pear]bool{},
		buffer: 100,
		logger: log.Printf,
	}

	// Apply options over default values.
	for _, o := range ops {
		o(b)
	}

	return b
}

// OptBuffer sets the message buffer size - number of messages that the server can keep for each
// connection. This buffer allows slow connections to digest the messages slower without delaying
// faster pears.
func OptBuffer(buffer int) func(*Beam) {
	return func(b *Beam) { b.buffer = buffer }
}

// OptUpgrader sets the websocket upgrader configuration that is used to upgrade incoming
// connections.
func OptUpgrader(upgrader websocket.Upgrader) func(*Beam) {
	return func(b *Beam) { b.upgrader = upgrader }
}

// OptHeaders set the headers for the HTTP response of a websocket connection.
func OptHeaders(headers http.Header) func(*Beam) {
	return func(b *Beam) { b.headers = headers }
}

// OptLogger sets the logger function. The default is standard go log, use `nil` to disable logging.
func OptLogger(logger func(string, ...interface{})) func(*Beam) {
	return func(b *Beam) { b.logger = logger }
}

type pear struct {
	ch   chan<- *websocket.PreparedMessage
	addr string
}

func (b *Beam) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ch := make(chan *websocket.PreparedMessage, b.buffer)
	p := &pear{
		addr: r.RemoteAddr,
		ch:   ch,
	}
	b.log(p, "New connection")

	b.add(p)
	defer b.remove(p)

	// Create a websocket connection with the client.
	conn, err := b.upgrader.Upgrade(w, r, b.headers)
	if err != nil {
		b.log(p, "Failed creating websocket: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	done := clientClosed(conn)

	defer conn.Close()
	defer b.log(p, "Disconnected")

	// Keep writing to the connection until it is closed.
	for {
		select {
		case v := <-ch:
			err := conn.WritePreparedMessage(v)
			if err != nil {
				b.log(p, "Failed writing to connection: %s", err)
				return
			}
		case <-done: // Wait for client to close the connection.
			b.log(p, "Client closed connection")
			return
		}
	}
}

// Send the data to all connected connections.
func (b *Beam) Send(data interface{}) error {
	buf, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed marshaling %v: %s", data, err)
	}

	msg, err := websocket.NewPreparedMessage(1, buf)
	if err != nil {
		return fmt.Errorf("failed preparing message %v: %s", buf, err)
	}

	var failed []string

	b.lock.Lock()
	defer b.lock.Unlock()
	for p := range b.pears {
		select {
		case p.ch <- msg:
		default:
			failed = append(failed, p.addr)
		}
	}

	if len(failed) > 0 {
		b.logger("Discarded buffer overflow message for %s", strings.Join(failed, ","))
	}
	return nil
}

func (c *Beam) add(p *pear) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.pears[p] = true
}

func (c *Beam) remove(p *pear) {
	c.lock.Lock()
	defer c.lock.Unlock()
	delete(c.pears, p)
}

// clientClosed return a channel that will be closed when the client is disconnected.
func clientClosed(conn *websocket.Conn) <-chan struct{} {
	done := make(chan struct{})

	// Read client messages to detect when client close the connection.
	go func() {
		defer close(done)
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	}()

	return done
}

func (b *Beam) log(p *pear, format string, args ...interface{}) {
	if b.logger == nil {
		return
	}
	b.logger("["+p.addr+"] "+format, args...)
}
