package lobby

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type testClient struct {
	t    *testing.T
	conn *websocket.Conn
	id   int
}

var testPolicy = VersionPolicy{
	MinVersion:    "1.0.0",
	LatestVersion: "1.2.0",
	UpdateURLs:    map[string]string{"android": "https://play.example/uma"},
}

func startServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(NewHub(testPolicy))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// connect dials the server and says hello with the latest version.
func connect(t *testing.T, url string) *testClient {
	t.Helper()
	c := dial(t, url)
	c.send(Message{Type: TypeHello, Version: "1.2.0", Platform: "linux"})
	if m := c.expect(TypeHelloOK); m.LatestVersion != "" {
		t.Fatalf("up-to-date client told to update: %+v", m)
	}
	return c
}

func dial(t *testing.T, url string) *testClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	c := &testClient{t: t, conn: conn}
	c.id = c.expect(TypeWelcome).PlayerID
	return c
}

func (c *testClient) send(m Message) {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, c.conn, m); err != nil {
		c.t.Fatal(err)
	}
}

func (c *testClient) expect(msgType string) Message {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var m Message
	if err := wsjson.Read(ctx, c.conn, &m); err != nil {
		c.t.Fatalf("waiting for %s: %v", msgType, err)
	}
	if m.Type != msgType {
		c.t.Fatalf("got %s (%s), want %s", m.Type, m.Error, msgType)
	}
	return m
}

// setupRoom creates a room hosted by the first client and joins the rest.
func setupRoom(t *testing.T, url string, n int) ([]*testClient, string) {
	t.Helper()
	host := connect(t, url)
	host.send(Message{Type: TypeCreateRoom, Name: "host"})
	code := host.expect(TypeRoomJoined).Code
	clients := []*testClient{host}
	for i := 1; i < n; i++ {
		c := connect(t, url)
		c.send(Message{Type: TypeJoinRoom, Name: "guest", Code: strings.ToLower(code)})
		joined := c.expect(TypeRoomJoined)
		if len(joined.Players) != i+1 || joined.HostID != host.id {
			t.Fatalf("bad room_joined: %+v", joined)
		}
		for _, other := range clients {
			if got := other.expect(TypePlayerJoined).PlayerID; got != c.id {
				t.Fatalf("player_joined for %d, want %d", got, c.id)
			}
		}
		clients = append(clients, c)
	}
	return clients, code
}

func TestRelay(t *testing.T) {
	c, _ := setupRoom(t, startServer(t), 3)
	payload := json.RawMessage(`{"tick":7}`)

	c[1].send(Message{Type: TypeRelay, Payload: payload})
	for _, other := range []*testClient{c[0], c[2]} {
		m := other.expect(TypeRelay)
		if m.From != c[1].id || string(m.Payload) != string(payload) {
			t.Fatalf("bad relay: %+v", m)
		}
	}

	c[0].send(Message{Type: TypeRelay, To: c[2].id, Payload: payload})
	if m := c[2].expect(TypeRelay); m.From != c[0].id {
		t.Fatalf("bad direct relay: %+v", m)
	}
}

func TestMatchRules(t *testing.T) {
	url := startServer(t)
	rooms, _ := setupRoom(t, url, 1)
	alone := rooms[0]
	alone.send(Message{Type: TypeStartMatch})
	alone.expect(TypeError)

	c, code := setupRoom(t, url, 2)
	c[1].send(Message{Type: TypeStartMatch})
	c[1].expect(TypeError) // not the host

	c[0].send(Message{Type: TypeStartMatch})
	c[0].expect(TypeMatchStarted)
	c[1].expect(TypeMatchStarted)

	late := connect(t, url)
	late.send(Message{Type: TypeJoinRoom, Name: "late", Code: code})
	late.expect(TypeError)

	c[0].send(Message{Type: TypeEndMatch, Payload: json.RawMessage(`{"winner":1}`)})
	c[0].expect(TypeMatchEnded)
	c[1].expect(TypeMatchEnded)

	// The room reopens between matches: the late player can join now and is
	// part of the next match.
	late.send(Message{Type: TypeJoinRoom, Name: "late", Code: code})
	if m := late.expect(TypeRoomJoined); len(m.Players) != 3 {
		t.Fatalf("late join: %+v", m)
	}
	c[0].expect(TypePlayerJoined)
	c[1].expect(TypePlayerJoined)
	c[0].send(Message{Type: TypeStartMatch})
	for _, p := range []*testClient{c[0], c[1], late} {
		if m := p.expect(TypeMatchStarted); len(m.Players) != 3 {
			t.Fatalf("rematch players: %+v", m)
		}
	}
}

func TestHostLeaves(t *testing.T) {
	c, _ := setupRoom(t, startServer(t), 3)
	c[0].conn.Close(websocket.StatusNormalClosure, "")

	for _, other := range c[1:] {
		if m := other.expect(TypePlayerLeft); m.PlayerID != c[0].id {
			t.Fatalf("bad player_left: %+v", m)
		}
		if m := other.expect(TypeHostChanged); m.HostID != c[1].id {
			t.Fatalf("bad host_changed: %+v", m)
		}
	}
}

func TestRoomFull(t *testing.T) {
	url := startServer(t)
	_, code := setupRoom(t, url, MaxPlayers)

	extra := connect(t, url)
	extra.send(Message{Type: TypeJoinRoom, Name: "extra", Code: code})
	if m := extra.expect(TypeError); m.Error != "room is full" {
		t.Fatalf("got %q", m.Error)
	}
	extra.send(Message{Type: TypeJoinRoom, Name: "extra", Code: "NOPE"})
	extra.expect(TypeError)
}

func TestValidation(t *testing.T) {
	c := connect(t, startServer(t))
	c.send(Message{Type: TypeCreateRoom, Name: "  "})
	c.expect(TypeError)
	c.send(Message{Type: TypeCreateRoom, Name: strings.Repeat("x", 17)})
	c.expect(TypeError)
	c.send(Message{Type: TypeRelay})
	c.expect(TypeError)
	c.send(Message{Type: "nonsense"})
	c.expect(TypeError)
}

func TestVersionCheck(t *testing.T) {
	url := startServer(t)

	c := dial(t, url)
	c.send(Message{Type: TypeCreateRoom, Name: "early"})
	c.expect(TypeError) // no hello yet

	c.send(Message{Type: TypeHello, Version: "0.9.5", Platform: "Android"})
	m := c.expect(TypeUpdateRequired)
	if m.MinVersion != "1.0.0" || m.LatestVersion != "1.2.0" || m.UpdateURL != "https://play.example/uma" {
		t.Fatalf("bad update_required: %+v", m)
	}
	c.send(Message{Type: TypeCreateRoom, Name: "still old"})
	c.expect(TypeError)

	// Allowed but outdated: can play, and hears about the newer build.
	c.send(Message{Type: TypeHello, Version: "1.1.0", Platform: "web"})
	if m := c.expect(TypeHelloOK); m.LatestVersion != "1.2.0" || m.UpdateURL != "" {
		t.Fatalf("bad hello_ok for outdated client: %+v", m)
	}
	c.send(Message{Type: TypeCreateRoom, Name: "ok"})
	c.expect(TypeRoomJoined)
}

func TestCompareVersions(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"1.2", "1.2.0", 0},
		{"v1.10.0", "1.9.9", 1},
		{"0.9.9", "1.0.0", -1},
		{"1.0.0-beta", "1.0.0", 0},
		{"garbage", "0.0.1", -1},
		{"", "0.1.0", -1},
	} {
		if got := compareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestVersionEndpoint(t *testing.T) {
	rec := httptest.NewRecorder()
	testPolicy.ServeHTTP(rec, httptest.NewRequest("GET", "/version", nil))
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("missing CORS header")
	}
	var got VersionPolicy
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.MinVersion != "1.0.0" || got.LatestVersion != "1.2.0" || got.UpdateURLs["android"] == "" {
		t.Fatalf("bad /version body: %+v", got)
	}
}
