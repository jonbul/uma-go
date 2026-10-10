package lobby

import "encoding/json"

// Message types sent by the client. Hello must come first: the server
// answers everything else with an error until it has accepted the version.
const (
	TypeHello      = "hello"       // version, platform
	TypeCreateRoom = "create_room" // name
	TypeJoinRoom   = "join_room"   // name, code
	TypeLeaveRoom  = "leave_room"
	TypeStartMatch = "start_match" // host only
	TypeEndMatch   = "end_match"   // host only, payload is relayed (e.g. the winner)
	TypeRelay      = "relay"       // to (0 = everyone else), payload
)

// Message types sent by the server.
const (
	TypeWelcome = "welcome" // player_id
	// Answers to hello. hello_ok may carry latest_version and update_url
	// when a newer build exists; update_required blocks the client.
	TypeHelloOK        = "hello_ok"        // latest_version, update_url
	TypeUpdateRequired = "update_required" // min_version, latest_version, update_url
	TypeRoomJoined     = "room_joined"     // code, host_id, players
	TypePlayerJoined   = "player_joined"   // player_id, name
	TypePlayerLeft     = "player_left"     // player_id
	TypeHostChanged    = "host_changed"    // host_id
	TypeMatchStarted   = "match_started"   // players
	TypeMatchEnded     = "match_ended"     // payload
	TypeError          = "error"           // error
	// TypeRelay is also sent by the server, with from set.
)

// Message is the JSON envelope for everything sent over the WebSocket.
// Player IDs start at 1, so a zero ID means "not set".
type Message struct {
	Type     string          `json:"type"`
	Name     string          `json:"name,omitempty"`
	Code     string          `json:"code,omitempty"`
	PlayerID int             `json:"player_id,omitempty"`
	HostID   int             `json:"host_id,omitempty"`
	To       int             `json:"to,omitempty"`
	From     int             `json:"from,omitempty"`
	Players  []PlayerInfo    `json:"players,omitempty"`
	Payload  json.RawMessage `json:"payload,omitempty"`
	Error    string          `json:"error,omitempty"`

	Version       string `json:"version,omitempty"`
	Platform      string `json:"platform,omitempty"`
	MinVersion    string `json:"min_version,omitempty"`
	LatestVersion string `json:"latest_version,omitempty"`
	UpdateURL     string `json:"update_url,omitempty"`
}

// PlayerInfo describes a player in a room.
type PlayerInfo struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}
