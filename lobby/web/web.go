// Package web embeds lobby static assets so they ship inside the binary.
package web

import _ "embed"

//go:embed leaderboard.html
var LeaderboardHTML []byte

//go:embed spectate.html
var SpectateHTML []byte

//go:embed admin.html
var AdminHTML []byte

//go:embed admin_login.html
var AdminLoginHTML []byte
