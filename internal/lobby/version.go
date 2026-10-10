package lobby

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// VersionPolicy decides which game builds may play. Clients older than
// MinVersion must update before connecting; clients older than
// LatestVersion may play but are told an update exists. UpdateURLs maps a
// platform ("android", "ios", "web", ...) to where its update is found.
type VersionPolicy struct {
	MinVersion    string            `json:"min_version"`
	LatestVersion string            `json:"latest_version"`
	UpdateURLs    map[string]string `json:"update_urls,omitempty"`
}

// check reports whether a client at version may play, and whether a newer
// build exists.
func (p VersionPolicy) check(version string) (allowed, outdated bool) {
	return compareVersions(version, p.MinVersion) >= 0, compareVersions(version, p.LatestVersion) < 0
}

func (p VersionPolicy) updateURL(platform string) string {
	return p.UpdateURLs[strings.ToLower(platform)]
}

// ServeHTTP answers GET /version so a client can check before connecting.
// CORS is open because web builds may be served from another origin.
func (p VersionPolicy) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(p)
}

// compareVersions compares "major.minor.patch" versions, returning -1, 0 or
// 1. Missing parts count as 0 and anything after a '-' or '+' is ignored; a
// part that isn't a number counts as 0, so a malformed version sorts low and
// gets asked to update.
func compareVersions(a, b string) int {
	pa, pb := versionParts(a), versionParts(b)
	for i := range pa {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func versionParts(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	var parts [3]int
	for i, s := range strings.SplitN(v, ".", 3) {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			n = 0
		}
		parts[i] = n
	}
	return parts
}
