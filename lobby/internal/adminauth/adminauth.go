// Package adminauth gates the lobby's /admin/* surface behind a
// Telegram-delivered OTP. Spec: docs/specs/foundations/04-lobby.md
// § Admin auth (Telegram OTP).
//
// Flow at a glance:
//  1. POST /admin/login → server creates a session_id (16 random bytes
//     encoded as 32 hex characters) + 6-digit OTP, persists
//     (session_id, sha256(session_id||otp), expires_at, 0 attempts) to
//     pending_admin_logins, sets admin_login cookie, sends OTP to
//     Telegram chat.
//  2. POST /admin/verify (cookie carries session_id, body carries
//     otp) → row consumed, admin_session HMAC cookie set, redirect
//     to /admin/.
//  3. /admin/* → middleware reads admin_session, verifies HMAC + exp,
//     pass-through or redirect to /admin/login.
//
// Storage lives in lobby.db (same SQLite file as the queue). The
// admin_session cookie is stateless — server only holds the HMAC
// secret, no per-session DB lookup on the hot path.
package adminauth

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/greyhats/defcon-game/pkg/netutil"
)

// Config bundles the env-driven parameters. An empty BotToken or
// ChatID disables the gate entirely (every /admin/* request returns
// 503 "admin disabled"); an empty SessionSecret hard-fails at New so
// a deploy can't accidentally serve cookies signed with the empty
// string.
type Config struct {
	BotToken      string
	ChatID        string
	SessionSecret []byte
	// Now is the time source; tests inject a fake clock. Defaults to
	// time.Now when nil.
	Now func() time.Time
	// HTTPClient sends Telegram requests. nil → http.DefaultClient
	// with a 5 s timeout.
	HTTPClient *http.Client
	// ForceSecureCookies forces Secure=true on every admin cookie
	// regardless of what the request looks like. Set in prod (where
	// the operator knows TLS is terminated upstream) so that a
	// misconfigured proxy that omits X-Forwarded-Proto can't silently
	// downgrade admin session cookies to non-Secure. Threaded from
	// the BEHIND_TLS env var by main.
	ForceSecureCookies bool
}

// Service is the adminauth root. Construct via New, then call Wrap to
// gate /admin/* and register the login/verify/logout HTTP handlers
// from main.
type Service struct {
	db   *sql.DB
	cfg  Config
	now  func() time.Time
	send func(ctx context.Context, text string) error
	rl   *loginRateLimiter
}

// SessionTTL controls how long an admin_session cookie is valid.
// Long enough that a moderation session doesn't get logged out
// mid-task; short enough that a stolen laptop has a bounded
// blast radius.
const SessionTTL = 24 * time.Hour

// otpTTL controls how long a pending OTP stays valid for verify.
// Spec: 5 min.
const otpTTL = 5 * time.Minute

// otpMaxAttempts caps brute-force attempts per session. With a 6-digit
// OTP that's 5/1_000_000 chance per session.
const otpMaxAttempts = 5

// loginRateMax / loginRateWindow rate-limit /admin/login by IP.
// 5 OTPs per 15 minutes per IP is comfortable for a real admin who
// retries a couple of times and tight against spam.
const (
	loginRateMax    = 5
	loginRateWindow = 15 * time.Minute
)

const (
	cookieLogin   = "admin_login"
	cookieSession = "admin_session"
)

// secureCookie reports whether response cookies should carry Secure.
// Returns true if the operator forced it on (BEHIND_TLS=1), if the
// connection itself is TLS, or if the immediate proxy terminated TLS
// upstream and forwarded X-Forwarded-Proto. Used by both issuance and
// deletion paths so the Set-Cookie attributes match — some browsers
// refuse to clear a Secure-issued cookie via a non-Secure Set-Cookie.
func (s *Service) secureCookie(r *http.Request) bool {
	if s.cfg.ForceSecureCookies {
		return true
	}
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// ErrDisabled is returned by hot-path methods when the gate is
// configured off (no bot token / chat). Callers should answer 503.
var ErrDisabled = errors.New("adminauth: disabled")

// ErrAuthFailed wraps every "this OTP attempt is bad" outcome from
// verifyOTP — wrong code, expired, locked out, no pending row. It
// lets HandleVerify distinguish these (→ 401 with a clean message
// stripped of the sentinel prefix) from DB / commit errors (→ 500),
// so an infrastructure failure can't be confused with a credential
// rejection.
var ErrAuthFailed = errors.New("auth failed")

// authFailed wraps a clean message into an ErrAuthFailed error.
func authFailed(msg string) error { return fmt.Errorf("%w: %s", ErrAuthFailed, msg) }

// New constructs a Service. Returns an error only on programmer
// mistakes (empty session secret with non-empty bot token, etc.); a
// fully-empty config returns a disabled-but-usable Service so the
// caller can wire routes unconditionally.
func New(db *sql.DB, cfg Config) (*Service, error) {
	if cfg.BotToken != "" && cfg.ChatID != "" && len(cfg.SessionSecret) < 16 {
		return nil, errors.New("adminauth: ADMIN_SESSION_SECRET must be ≥ 16 bytes when admin is enabled")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	}
	s := &Service{
		db:  db,
		cfg: cfg,
		now: cfg.Now,
		rl:  newLoginRateLimiter(loginRateMax, loginRateWindow, cfg.Now),
	}
	s.send = s.sendTelegram
	return s, nil
}

// Enabled reports whether admin auth is configured. When false, all
// /admin/* endpoints return 503.
func (s *Service) Enabled() bool {
	return s.cfg.BotToken != "" && s.cfg.ChatID != "" && len(s.cfg.SessionSecret) >= 16
}

// EnsureSchema creates pending_admin_logins. Idempotent.
func EnsureSchema(db *sql.DB) error {
	_, err := db.Exec(`
        CREATE TABLE IF NOT EXISTS pending_admin_logins (
            session_id   TEXT PRIMARY KEY,
            otp_hash     BLOB NOT NULL,
            expires_at   INTEGER NOT NULL,
            attempts     INTEGER NOT NULL DEFAULT 0,
            created_at   INTEGER NOT NULL
        );
        CREATE INDEX IF NOT EXISTS idx_pending_admin_expires ON pending_admin_logins(expires_at);
    `)
	return err
}

// ----- session cookies ---------------------------------------------------

// signSession formats `<exp_unix>.<base64url(hmac_sha256(secret, exp_unix))>`.
// The exp value is the only payload — anyone with the chat is admin,
// so we don't need to bind the cookie to a specific identity.
func (s *Service) signSession(exp time.Time) string {
	body := strconv.FormatInt(exp.Unix(), 10)
	mac := hmac.New(sha256.New, s.cfg.SessionSecret)
	mac.Write([]byte(body))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return body + "." + sig
}

// verifySession returns nil iff the cookie is well-formed, has a
// valid HMAC, and hasn't expired.
func (s *Service) verifySession(cookieValue string) error {
	dot := strings.IndexByte(cookieValue, '.')
	if dot <= 0 || dot == len(cookieValue)-1 {
		return errors.New("malformed session cookie")
	}
	body := cookieValue[:dot]
	sig := cookieValue[dot+1:]
	expUnix, err := strconv.ParseInt(body, 10, 64)
	if err != nil {
		return errors.New("malformed exp")
	}
	if s.now().Unix() >= expUnix {
		return errors.New("expired")
	}
	mac := hmac.New(sha256.New, s.cfg.SessionSecret)
	mac.Write([]byte(body))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) != 1 {
		return errors.New("bad signature")
	}
	return nil
}

// ----- OTP issue / verify ------------------------------------------------

// issueOTP generates a session_id + 6-digit code, persists the row,
// and returns both. The OTP itself is sent to Telegram by the caller.
func (s *Service) issueOTP(ctx context.Context) (sessionID, otp string, err error) {
	sessionID, err = randomHex(16)
	if err != nil {
		return "", "", fmt.Errorf("session id: %w", err)
	}
	otp, err = randomDigits(6)
	if err != nil {
		return "", "", fmt.Errorf("otp: %w", err)
	}
	hash := otpHash(sessionID, otp)
	now := s.now()
	expires := now.Add(otpTTL).Unix()
	if _, err := s.db.ExecContext(ctx, `
        INSERT INTO pending_admin_logins (session_id, otp_hash, expires_at, attempts, created_at)
        VALUES (?, ?, ?, 0, ?)
    `, sessionID, hash, expires, now.Unix()); err != nil {
		return "", "", fmt.Errorf("insert: %w", err)
	}
	// Best-effort sweep — unbounded growth would only matter under
	// abuse, but a constant-time delete here keeps the table tidy.
	_, _ = s.db.ExecContext(ctx, `DELETE FROM pending_admin_logins WHERE expires_at < ?`, now.Unix())
	return sessionID, otp, nil
}

// verifyOTP checks the submitted code against the stored hash. On a
// successful match the row is deleted (one-shot). On too many
// attempts or expiry the row is also deleted so retry forces a new
// /admin/login.
//
// Returns ErrAuthFailed-wrapped errors for credential rejections;
// any other error is a DB / commit failure and the caller should
// surface 500. We never send a 401 on commit failure because that
// would let the user retry the same OTP (the row hasn't been
// consumed) under the impression they got the code wrong.
func (s *Service) verifyOTP(ctx context.Context, sessionID, otp string) error {
	if sessionID == "" || otp == "" {
		return authFailed("missing session or otp")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var (
		hash     []byte
		expires  int64
		attempts int
	)
	err = tx.QueryRowContext(ctx,
		`SELECT otp_hash, expires_at, attempts FROM pending_admin_logins WHERE session_id = ?`,
		sessionID,
	).Scan(&hash, &expires, &attempts)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return authFailed("no pending login")
		}
		return fmt.Errorf("lookup: %w", err)
	}
	now := s.now()
	if now.Unix() >= expires {
		if _, err := tx.ExecContext(ctx, `DELETE FROM pending_admin_logins WHERE session_id = ?`, sessionID); err != nil {
			return fmt.Errorf("expire delete: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("expire commit: %w", err)
		}
		return authFailed("otp expired")
	}
	if attempts >= otpMaxAttempts {
		if _, err := tx.ExecContext(ctx, `DELETE FROM pending_admin_logins WHERE session_id = ?`, sessionID); err != nil {
			return fmt.Errorf("lockout delete: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("lockout commit: %w", err)
		}
		return authFailed("too many attempts")
	}
	got := otpHash(sessionID, otp)
	if subtle.ConstantTimeCompare(hash, got) != 1 {
		if _, err := tx.ExecContext(ctx,
			`UPDATE pending_admin_logins SET attempts = attempts + 1 WHERE session_id = ?`,
			sessionID); err != nil {
			return fmt.Errorf("bump attempts: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("bump commit: %w", err)
		}
		return authFailed("invalid otp")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM pending_admin_logins WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("consume: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("consume commit: %w", err)
	}
	return nil
}

func otpHash(sessionID, otp string) []byte {
	h := sha256.New()
	h.Write([]byte(sessionID))
	h.Write([]byte{0})
	h.Write([]byte(otp))
	return h.Sum(nil)
}

// ----- Telegram ----------------------------------------------------------

func (s *Service) sendTelegram(ctx context.Context, text string) error {
	if !s.Enabled() {
		return ErrDisabled
	}
	endpoint := "https://api.telegram.org/bot" + url.PathEscape(s.cfg.BotToken) + "/sendMessage"
	// parse_mode=Markdown so backtick-wrapped substrings render as
	// inline code — Telegram's mobile clients then expose tap-to-copy
	// on those spans, which is the whole point for an OTP. Callers
	// are responsible for escaping any user-controlled text; today the
	// only message is the OTP delivery string composed below from a
	// hard-coded template + 6 random digits, so there's nothing to
	// escape.
	body, err := json.Marshal(map[string]string{
		"chat_id":    s.cfg.ChatID,
		"text":       text,
		"parse_mode": "Markdown",
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.cfg.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("telegram %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return nil
}

// ----- HTTP handlers / middleware ---------------------------------------

// Wrap returns a handler that gates next behind a valid admin_session
// cookie. Failed checks redirect to /admin/login, except when the
// request looks like an XHR/JSON call (Accept doesn't include text/html),
// in which case we 401 so a stale fetch doesn't get HTML back.
//
// Mutating requests (anything but GET / HEAD) additionally require the
// Origin header's host to match the request Host. SameSite=Strict on
// admin_session blocks most cross-site requests already; the Origin
// check is the explicit second layer for the cases SameSite misses
// (subdomains, Lax/Strict ambiguities in older browsers, etc.) and
// closes the issue 71 #9 CSRF gap on /admin/leaderboard/{flag,unflag},
// /admin/replay/:id/ticket, and the admin DELETE routes.
func (s *Service) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.Enabled() {
			http.Error(w, "admin disabled", http.StatusServiceUnavailable)
			return
		}
		c, err := r.Cookie(cookieSession)
		if err != nil || c.Value == "" {
			s.unauthorized(w, r)
			return
		}
		if err := s.verifySession(c.Value); err != nil {
			s.unauthorized(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !sameOrigin(r) {
				http.Error(w, "cross-origin admin request rejected", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// sameOrigin reports whether the Origin header's host matches r.Host.
// Returns false if Origin is missing — mutating admin requests must
// carry it (every modern browser does on POST/DELETE, and curl-style
// callers can set it explicitly). Falls back to Referer's host when
// Origin is absent; some older corporate proxies strip Origin but not
// Referer, and admins shouldn't be locked out by that.
func sameOrigin(r *http.Request) bool {
	want := r.Host
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		return u.Host == want
	}
	if ref := r.Header.Get("Referer"); ref != "" {
		u, err := url.Parse(ref)
		if err != nil {
			return false
		}
		return u.Host == want
	}
	return false
}

func (s *Service) unauthorized(w http.ResponseWriter, r *http.Request) {
	if !strings.Contains(r.Header.Get("Accept"), "text/html") {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// Clear any stale session cookie so the login page renders fresh.
	http.SetCookie(w, &http.Cookie{
		Name:     cookieSession,
		Value:    "",
		Path:     "/admin",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secureCookie(r),
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// ServeLoginPage renders the login HTML. Public — no auth.
func (s *Service) ServeLoginPage(w http.ResponseWriter, r *http.Request, html []byte) {
	if !s.Enabled() {
		http.Error(w, "admin disabled", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(html)
}

// HandleLogin handles POST /admin/login. Issues an OTP, sends to
// Telegram, sets the admin_login cookie. Idempotent rate-limit on IP.
func (s *Service) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.Enabled() {
		http.Error(w, "admin disabled", http.StatusServiceUnavailable)
		return
	}
	ip := netutil.ClientIP(r)
	if !s.rl.allow(ip) {
		http.Error(w, "rate limit", http.StatusTooManyRequests)
		return
	}
	sessionID, otp, err := s.issueOTP(r.Context())
	if err != nil {
		log.Printf("adminauth: issue: %v", err)
		http.Error(w, "issue failed", http.StatusInternalServerError)
		return
	}
	// Wrapping the OTP in backticks renders it as inline code in
	// Telegram, which gives the recipient tap-to-copy on mobile.
	msg := fmt.Sprintf("VIBECAT 404 admin login code: `%s`\n(valid 5 min)", otp)
	if err := s.send(r.Context(), msg); err != nil {
		log.Printf("adminauth: telegram: %v", err)
		// Best-effort cleanup of the issued row so the operator can
		// retry without rate-limit drift.
		_, _ = s.db.ExecContext(r.Context(), `DELETE FROM pending_admin_logins WHERE session_id = ?`, sessionID)
		http.Error(w, "telegram send failed", http.StatusBadGateway)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieLogin,
		Value:    sessionID,
		Path:     "/admin",
		Expires:  s.now().Add(otpTTL),
		MaxAge:   int(otpTTL.Seconds()),
		HttpOnly: true,
		Secure:   s.secureCookie(r),
		SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

// HandleVerify handles POST /admin/verify. Reads OTP from form/JSON
// + session_id from cookie, consumes the row on success, sets the
// admin_session cookie, returns 204.
func (s *Service) HandleVerify(w http.ResponseWriter, r *http.Request) {
	if !s.Enabled() {
		http.Error(w, "admin disabled", http.StatusServiceUnavailable)
		return
	}
	c, err := r.Cookie(cookieLogin)
	if err != nil || c.Value == "" {
		http.Error(w, "no pending login", http.StatusBadRequest)
		return
	}
	// Cap the body before any parsing. FormValue triggers ParseForm
	// which would otherwise read up to defaultMaxMemory (10 MiB) of
	// form data into memory. 1 KiB is well above any real OTP payload
	// (form: "otp=123456" ≈ 10 B; JSON: ~16 B). Wrapping r.Body once
	// covers both the FormValue and the JSON fallback paths.
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	otp := strings.TrimSpace(r.FormValue("otp"))
	if otp == "" {
		// Fall back to JSON body for fetch() callers. r.Body is at EOF
		// after FormValue if the request was form-encoded, so this only
		// reads anything for non-form (JSON) requests, which is exactly
		// what we want.
		var body struct {
			OTP string `json:"otp"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		otp = strings.TrimSpace(body.OTP)
	}
	if otp == "" {
		http.Error(w, "otp required", http.StatusBadRequest)
		return
	}
	if err := s.verifyOTP(r.Context(), c.Value, otp); err != nil {
		if errors.Is(err, ErrAuthFailed) {
			// Strip the "auth failed: " prefix so the caller sees a
			// clean message in the body.
			msg := strings.TrimPrefix(err.Error(), ErrAuthFailed.Error()+": ")
			http.Error(w, msg, http.StatusUnauthorized)
			return
		}
		log.Printf("adminauth: verify: %v", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	exp := s.now().Add(SessionTTL)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieSession,
		Value:    s.signSession(exp),
		Path:     "/admin",
		Expires:  exp,
		MaxAge:   int(SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   s.secureCookie(r),
		// Strict so the admin_session cookie is never carried on
		// cross-site navigations / form posts. Combined with the Wrap
		// middleware's Origin check this closes the issue 71 #9 CSRF
		// gap on /admin/* mutating routes.
		SameSite: http.SameSiteStrictMode,
	})
	// Clear admin_login — it's been consumed.
	http.SetCookie(w, &http.Cookie{
		Name:     cookieLogin,
		Value:    "",
		Path:     "/admin",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secureCookie(r),
		SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

// HandleLogout clears admin_session.
func (s *Service) HandleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieSession,
		Value:    "",
		Path:     "/admin",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secureCookie(r),
		// Match the Strict attribute used at issuance — Chrome/Safari
		// refuse to clear a Strict cookie via a Lax Set-Cookie.
		SameSite: http.SameSiteStrictMode,
	})
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ----- helpers -----------------------------------------------------------

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// randomDigits returns n cryptographically-random decimal digits as a
// string. Each digit is sampled uniformly from 0..9 by rejection of
// the upper bias band of a uint8 (250..255 — 250 is the largest
// multiple of 10 ≤ 256, so anything ≥ 250 is rejected to keep the
// distribution unbiased without leaking via length).
func randomDigits(n int) (string, error) {
	out := make([]byte, n)
	buf := make([]byte, 1)
	for i := 0; i < n; {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		if buf[0] >= 250 { // 250..255 — reject; 6/256 = ~2.3% rate
			continue
		}
		out[i] = '0' + (buf[0] % 10)
		i++
	}
	return string(out), nil
}

// ----- rate limiter ------------------------------------------------------

type loginRateLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	now    func() time.Time
	hits   map[string][]time.Time
}

func newLoginRateLimiter(max int, window time.Duration, now func() time.Time) *loginRateLimiter {
	return &loginRateLimiter{
		max:    max,
		window: window,
		now:    now,
		hits:   map[string][]time.Time{},
	}
}

func (r *loginRateLimiter) allow(ip string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	cutoff := now.Add(-r.window)
	stamps := r.hits[ip]
	kept := stamps[:0]
	for _, t := range stamps {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= r.max {
		r.hits[ip] = kept
		return false
	}
	r.hits[ip] = append(kept, now)
	r.gcLocked(cutoff)
	return true
}

// gcLocked drops keys whose entire hit list is older than cutoff.
// Inline with the existing lock so a churn of distinct IPs (or a
// pre-netutil XFF spoof) can't grow the map without bound. The size
// gate keeps tiny deployments off the scan path.
func (r *loginRateLimiter) gcLocked(cutoff time.Time) {
	if len(r.hits) < 64 {
		return
	}
	for ip, stamps := range r.hits {
		live := false
		for _, t := range stamps {
			if t.After(cutoff) {
				live = true
				break
			}
		}
		if !live {
			delete(r.hits, ip)
		}
	}
}
