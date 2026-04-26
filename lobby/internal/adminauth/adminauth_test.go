package adminauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/greyhats/defcon-game/lobby/internal/queue"
)

// newServiceTest spins up a service with a temp lobby.db, fixed clock,
// and an in-memory Telegram fake. The fake records the latest message
// so tests can extract the OTP without going to the real API.
type tgFake struct {
	last string
	err  error
}

func newServiceTest(t *testing.T) (*Service, *tgFake, func() time.Time) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "lobby.db")
	db, err := queue.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return clock }
	cfg := Config{
		BotToken:      "test-token",
		ChatID:        "12345",
		SessionSecret: []byte("test-session-secret-must-be-long-enough"),
		Now:           now,
	}
	svc, err := New(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	fake := &tgFake{}
	svc.send = func(_ context.Context, text string) error {
		if fake.err != nil {
			return fake.err
		}
		fake.last = text
		return nil
	}
	return svc, fake, now
}

func extractOTP(t *testing.T, msg string) string {
	t.Helper()
	// Message format: "VIBECAT 404 admin login code: 123456\n..."
	const marker = "code: "
	i := strings.Index(msg, marker)
	if i < 0 {
		t.Fatalf("no code marker in: %q", msg)
	}
	rest := msg[i+len(marker):]
	if len(rest) < 6 {
		t.Fatalf("short code in: %q", msg)
	}
	return rest[:6]
}

func TestNewRejectsShortSecretWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	db, _ := queue.Open(filepath.Join(dir, "lobby.db"))
	defer db.Close()
	_, err := New(db, Config{
		BotToken:      "x",
		ChatID:        "y",
		SessionSecret: []byte("short"),
	})
	if err == nil {
		t.Error("expected error for too-short session secret")
	}
}

func TestServiceDisabledWhenUnconfigured(t *testing.T) {
	dir := t.TempDir()
	db, _ := queue.Open(filepath.Join(dir, "lobby.db"))
	defer db.Close()
	svc, err := New(db, Config{}) // empty config = disabled
	if err != nil {
		t.Fatal(err)
	}
	if svc.Enabled() {
		t.Error("Enabled() = true on empty config")
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	svc.Wrap(http.NotFoundHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("disabled Wrap: got %d, want 503", rec.Code)
	}
}

func TestSessionCookieRoundTrip(t *testing.T) {
	svc, _, now := newServiceTest(t)
	exp := now().Add(SessionTTL)
	cookie := svc.signSession(exp)
	if err := svc.verifySession(cookie); err != nil {
		t.Errorf("fresh cookie verifies: %v", err)
	}

	// Tamper with the body.
	if err := svc.verifySession("999999999." + strings.SplitN(cookie, ".", 2)[1]); err == nil {
		t.Error("tampered exp should not verify")
	}

	// Expired cookie.
	svc.now = func() time.Time { return exp.Add(time.Second) }
	if err := svc.verifySession(cookie); err == nil {
		t.Error("expired cookie should not verify")
	}
}

func TestOTPHappyPath(t *testing.T) {
	svc, fake, _ := newServiceTest(t)

	// Step 1: login.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/login", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	svc.HandleLogin(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("login: got %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	loginCookies := rec.Result().Cookies()
	var loginCookie *http.Cookie
	for _, c := range loginCookies {
		if c.Name == cookieLogin {
			loginCookie = c
		}
	}
	if loginCookie == nil {
		t.Fatal("admin_login cookie not set")
	}
	otp := extractOTP(t, fake.last)

	// Step 2: verify.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/admin/verify",
		strings.NewReader(`{"otp":"`+otp+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(loginCookie)
	svc.HandleVerify(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("verify: got %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	var sessionCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == cookieSession {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("admin_session cookie not set")
	}
	if err := svc.verifySession(sessionCookie.Value); err != nil {
		t.Errorf("session cookie should verify: %v", err)
	}

	// Step 3: gated handler now serves through.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/admin/leaderboard.json", nil)
	req.AddCookie(sessionCookie)
	wrapped := svc.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("OK"))
	}))
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "OK" {
		t.Errorf("gated handler: code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestOTPWrongCodeBumpsAttempts(t *testing.T) {
	svc, fake, _ := newServiceTest(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/login", nil)
	req.RemoteAddr = "1.2.3.4:5"
	svc.HandleLogin(rec, req)
	loginCookie := pickCookie(rec.Result().Cookies(), cookieLogin)
	correct := extractOTP(t, fake.last)

	// 5 wrong attempts, then on the 6th even the correct OTP fails
	// because the row was deleted at attempt 5.
	wrong := "000000"
	if wrong == correct {
		wrong = "111111"
	}
	for i := 0; i < otpMaxAttempts; i++ {
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/admin/verify",
			strings.NewReader(`{"otp":"`+wrong+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(loginCookie)
		svc.HandleVerify(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("attempt %d: got %d, want 401", i, rec.Code)
		}
	}
	// Now even the correct OTP should be rejected.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/admin/verify",
		strings.NewReader(`{"otp":"`+correct+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(loginCookie)
	svc.HandleVerify(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("after lockout, correct OTP: got %d, want 401", rec.Code)
	}
}

func TestOTPExpiry(t *testing.T) {
	svc, fake, _ := newServiceTest(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/login", nil)
	req.RemoteAddr = "9.9.9.9:1"
	svc.HandleLogin(rec, req)
	loginCookie := pickCookie(rec.Result().Cookies(), cookieLogin)
	otp := extractOTP(t, fake.last)

	// Advance the clock past expiry.
	svc.now = func() time.Time { return time.Date(2026, 4, 26, 12, 6, 0, 0, time.UTC) }

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/admin/verify",
		strings.NewReader(`{"otp":"`+otp+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(loginCookie)
	svc.HandleVerify(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expired OTP: got %d, want 401", rec.Code)
	}
}

func TestRateLimit(t *testing.T) {
	svc, _, _ := newServiceTest(t)
	const ip = "8.8.8.8:1"
	for i := 0; i < loginRateMax; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/login", nil)
		req.RemoteAddr = ip
		svc.HandleLogin(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("login %d: got %d, want 204", i, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/login", nil)
	req.RemoteAddr = ip
	svc.HandleLogin(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("rate-limited: got %d, want 429", rec.Code)
	}
}

func TestTelegramFailureRollsBack(t *testing.T) {
	svc, fake, _ := newServiceTest(t)
	fake.err = errors.New("telegram down")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/login", nil)
	req.RemoteAddr = "5.5.5.5:1"
	svc.HandleLogin(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("telegram fail: got %d, want 502", rec.Code)
	}
	// Pending row was cleaned up — confirm by issuing a fresh OTP and
	// checking only one row exists.
	fake.err = nil
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/admin/login", nil)
	req.RemoteAddr = "5.5.5.5:1"
	svc.HandleLogin(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("recovery login: got %d, want 204", rec.Code)
	}
	var n int
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM pending_admin_logins`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("pending rows after rollback+retry = %d, want 1", n)
	}
}

func TestWrapRedirectsHTMLClients(t *testing.T) {
	svc, _, _ := newServiceTest(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	req.Header.Set("Accept", "text/html")
	svc.Wrap(http.NotFoundHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("html client unauthorized: got %d, want 303", rec.Code)
	}
	if loc := rec.Result().Header.Get("Location"); loc != "/admin/login" {
		t.Errorf("redirect target = %q, want /admin/login", loc)
	}
}

func TestWrap401sJSONClients(t *testing.T) {
	svc, _, _ := newServiceTest(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/leaderboard.json", nil)
	req.Header.Set("Accept", "application/json")
	svc.Wrap(http.NotFoundHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("json client unauthorized: got %d, want 401", rec.Code)
	}
}

func TestRandomDigitsLength(t *testing.T) {
	for _, n := range []int{1, 6, 12} {
		s, err := randomDigits(n)
		if err != nil {
			t.Fatal(err)
		}
		if len(s) != n {
			t.Errorf("length: got %d, want %d", len(s), n)
		}
		for _, b := range s {
			if b < '0' || b > '9' {
				t.Errorf("non-digit byte: %q in %q", b, s)
			}
		}
	}
}

func pickCookie(cs []*http.Cookie, name string) *http.Cookie {
	for _, c := range cs {
		if c.Name == name {
			return c
		}
	}
	return nil
}
