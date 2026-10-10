# Changes

Server changes, newest first. The version matches `metadata.properties`.

## 0.1.0

- WebSocket lobby: rooms with 4-letter codes, 2-10 players, host-only
  start/end of matches, relay of game messages between players.
- Version check: clients must say `hello` with their version first;
  `-min-version` / `-latest-version` flags and `GET /version`.
- In-memory only, no database.
