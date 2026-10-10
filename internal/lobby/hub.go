// Package lobby runs rooms for online matches and relays messages between
// the players in them. It knows nothing about fighting: the room's host
// (a Godot client) runs the authoritative game and the server only forwards
// its messages. Everything lives in memory.
package lobby

import (
	"math/rand/v2"
	"strings"
	"sync"
)

const (
	MaxPlayers    = 10
	MinPlayers    = 2
	maxNameLength = 16
	codeLength    = 4
	// No I or O, so codes can't be confused with 1 or 0.
	codeLetters = "ABCDEFGHJKLMNPQRSTUVWXYZ"
)

// Hub owns every room and connected client. One mutex guards all of it:
// handlers are short and sends never block.
type Hub struct {
	mu      sync.Mutex
	rooms   map[string]*room
	nextID  int
	version VersionPolicy
}

type room struct {
	code    string
	hostID  int
	players []*client // in join order
	started bool
}

type client struct {
	id   int
	name string
	room *room
	// helloOK is set once the client's version has been accepted.
	helloOK bool
	send    chan Message
	// drop disconnects the client; used when its send buffer fills up.
	drop func()
}

func NewHub(version VersionPolicy) *Hub {
	return &Hub{rooms: make(map[string]*room), version: version}
}

func (h *Hub) newClient(drop func()) *client {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	return &client{id: h.nextID, send: make(chan Message, 64), drop: drop}
}

func (h *Hub) handle(c *client, m Message) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if m.Type == TypeHello {
		h.hello(c, m)
		return
	}
	if !c.helloOK {
		c.sendError("send hello with your version first")
		return
	}

	switch m.Type {
	case TypeCreateRoom:
		h.createRoom(c, m.Name)
	case TypeJoinRoom:
		h.joinRoom(c, m.Name, m.Code)
	case TypeLeaveRoom:
		h.leaveRoom(c)
	case TypeStartMatch:
		h.startMatch(c)
	case TypeEndMatch:
		h.endMatch(c, m)
	case TypeRelay:
		h.relay(c, m)
	default:
		c.sendError("unknown message type: " + m.Type)
	}
}

func (h *Hub) hello(c *client, m Message) {
	allowed, outdated := h.version.check(m.Version)
	url := h.version.updateURL(m.Platform)
	if !allowed {
		c.push(Message{
			Type: TypeUpdateRequired, MinVersion: h.version.MinVersion,
			LatestVersion: h.version.LatestVersion, UpdateURL: url,
		})
		return
	}
	c.helloOK = true
	reply := Message{Type: TypeHelloOK}
	if outdated {
		reply.LatestVersion = h.version.LatestVersion
		reply.UpdateURL = url
	}
	c.push(reply)
}

func (h *Hub) disconnect(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.leaveRoom(c)
}

func (h *Hub) createRoom(c *client, name string) {
	if !h.setName(c, name) {
		return
	}
	code := h.newCode()
	r := &room{code: code, hostID: c.id, players: []*client{c}}
	h.rooms[code] = r
	c.room = r
	c.push(r.joinedMessage())
}

func (h *Hub) joinRoom(c *client, name, code string) {
	if !h.setName(c, name) {
		return
	}
	r, ok := h.rooms[strings.ToUpper(strings.TrimSpace(code))]
	switch {
	case !ok:
		c.sendError("room not found")
		return
	case r.started:
		c.sendError("match already started")
		return
	case len(r.players) >= MaxPlayers:
		c.sendError("room is full")
		return
	}
	r.broadcast(Message{Type: TypePlayerJoined, PlayerID: c.id, Name: c.name}, 0)
	r.players = append(r.players, c)
	c.room = r
	c.push(r.joinedMessage())
}

// setName validates the name and checks the client isn't in a room yet.
func (h *Hub) setName(c *client, name string) bool {
	if c.room != nil {
		c.sendError("already in a room")
		return false
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > maxNameLength {
		c.sendError("name must be 1-16 characters")
		return false
	}
	c.name = name
	return true
}

func (h *Hub) leaveRoom(c *client) {
	r := c.room
	if r == nil {
		return
	}
	c.room = nil
	for i, p := range r.players {
		if p == c {
			r.players = append(r.players[:i], r.players[i+1:]...)
			break
		}
	}
	if len(r.players) == 0 {
		delete(h.rooms, r.code)
		return
	}
	r.broadcast(Message{Type: TypePlayerLeft, PlayerID: c.id}, 0)
	if r.hostID == c.id {
		// Host migration mid-match isn't supported: the game should end the
		// match when it sees host_changed while one is running.
		r.hostID = r.players[0].id
		r.started = false
		r.broadcast(Message{Type: TypeHostChanged, HostID: r.hostID}, 0)
	}
}

func (h *Hub) startMatch(c *client) {
	r := c.room
	switch {
	case r == nil || r.hostID != c.id:
		c.sendError("only the host can start the match")
	case r.started:
		c.sendError("match already started")
	case len(r.players) < MinPlayers:
		c.sendError("need at least 2 players")
	default:
		r.started = true
		r.broadcast(Message{Type: TypeMatchStarted, Players: r.playerInfo()}, 0)
	}
}

func (h *Hub) endMatch(c *client, m Message) {
	r := c.room
	if r == nil || r.hostID != c.id || !r.started {
		c.sendError("only the host can end a running match")
		return
	}
	r.started = false
	r.broadcast(Message{Type: TypeMatchEnded, Payload: m.Payload}, 0)
}

func (h *Hub) relay(c *client, m Message) {
	r := c.room
	if r == nil {
		c.sendError("not in a room")
		return
	}
	out := Message{Type: TypeRelay, From: c.id, Payload: m.Payload}
	if m.To == 0 {
		r.broadcast(out, c.id)
		return
	}
	for _, p := range r.players {
		if p.id == m.To {
			p.push(out)
			return
		}
	}
	c.sendError("player not in room")
}

func (h *Hub) newCode() string {
	for {
		b := make([]byte, codeLength)
		for i := range b {
			b[i] = codeLetters[rand.IntN(len(codeLetters))]
		}
		if _, taken := h.rooms[string(b)]; !taken {
			return string(b)
		}
	}
}

// broadcast sends m to every player except skipID (0 skips nobody).
func (r *room) broadcast(m Message, skipID int) {
	for _, p := range r.players {
		if p.id != skipID {
			p.push(m)
		}
	}
}

func (r *room) playerInfo() []PlayerInfo {
	info := make([]PlayerInfo, len(r.players))
	for i, p := range r.players {
		info[i] = PlayerInfo{ID: p.id, Name: p.name}
	}
	return info
}

func (r *room) joinedMessage() Message {
	return Message{Type: TypeRoomJoined, Code: r.code, HostID: r.hostID, Players: r.playerInfo()}
}

// push queues m without blocking; a client that can't keep up is dropped.
func (c *client) push(m Message) {
	select {
	case c.send <- m:
	default:
		c.drop()
	}
}

func (c *client) sendError(text string) {
	c.push(Message{Type: TypeError, Error: text})
}
