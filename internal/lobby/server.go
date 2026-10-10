package lobby

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const writeTimeout = 5 * time.Second

// ServeHTTP upgrades the request to a WebSocket and runs the client until it
// disconnects.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Native Godot clients send no Origin header; a browser (web export)
		// does. Restrict this once there is a known web host.
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	c := h.newClient(cancel)
	defer h.disconnect(c)
	slog.Info("client connected", "id", c.id, "remote", r.RemoteAddr)
	defer slog.Info("client disconnected", "id", c.id)

	go writeLoop(ctx, conn, c)
	c.push(Message{Type: TypeWelcome, PlayerID: c.id})

	for {
		var m Message
		if err := wsjson.Read(ctx, conn, &m); err != nil {
			return
		}
		h.handle(c, m)
	}
}

func writeLoop(ctx context.Context, conn *websocket.Conn, c *client) {
	for {
		select {
		case <-ctx.Done():
			return
		case m := <-c.send:
			writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := wsjson.Write(writeCtx, conn, m)
			cancel()
			if err != nil {
				c.drop()
				return
			}
		}
	}
}
