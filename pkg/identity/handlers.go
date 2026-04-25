package identity

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"
)

// Service wires the identity HTTP endpoints against a shared DB.
type Service struct {
	DB            *sql.DB
	RegisterLimit *RateLimiter
	VerifyLimit   *RateLimiter
	LoginLimit    *RateLimiter
}

// NewService builds the service with the spec-mandated rate-limit cooldowns.
func NewService(db *sql.DB) *Service {
	return &Service{
		DB:            db,
		RegisterLimit: NewRateLimiter(3 * time.Second),
		VerifyLimit:   NewRateLimiter(1 * time.Second),
		LoginLimit:    NewRateLimiter(1 * time.Second),
	}
}

// Register mounts /api/register, /api/verify, and /api/login on mux.
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/register", s.handleRegister)
	mux.HandleFunc("/api/verify", s.handleVerify)
	mux.HandleFunc("/api/login", s.handleLogin)
}

type registerReq struct {
	DisplayName string `json:"display_name"`
	Contact     string `json:"contact"`
	Password    string `json:"password"`
}

type registerResp struct {
	PlayerID string `json:"player_id"`
}

type verifyReq struct {
	PlayerID string `json:"player_id"`
	Password string `json:"password"`
}

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResp struct {
	PlayerID    string `json:"player_id"`
	DisplayName string `json:"display_name"`
}

type errResp struct {
	Error string `json:"error"`
}

func (s *Service) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.RegisterLimit.Allow(ClientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, errResp{Error: "slow down"})
		return
	}
	var req registerReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp{Error: "invalid json"})
		return
	}
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.Contact = strings.TrimSpace(req.Contact)
	if len(req.DisplayName) < 1 || len(req.DisplayName) > 32 {
		writeJSON(w, http.StatusBadRequest, errResp{Error: "display_name must be 1-32 chars"})
		return
	}
	if len(req.Contact) < 3 || len(req.Contact) > 256 {
		writeJSON(w, http.StatusBadRequest, errResp{Error: "contact must be 3-256 chars"})
		return
	}
	if len(req.Password) < 6 || len(req.Password) > 128 {
		writeJSON(w, http.StatusBadRequest, errResp{Error: "password must be 6-128 chars"})
		return
	}

	hash, err := HashPassword(req.Password)
	if err != nil {
		log.Printf("identity: hash: %v", err)
		writeJSON(w, http.StatusInternalServerError, errResp{Error: "server error"})
		return
	}
	id, err := NewPlayerID()
	if err != nil {
		log.Printf("identity: new id: %v", err)
		writeJSON(w, http.StatusInternalServerError, errResp{Error: "server error"})
		return
	}
	if err := Insert(s.DB, &Player{
		PlayerID:     id,
		DisplayName:  req.DisplayName,
		PasswordHash: hash,
		Contact:      req.Contact,
		RegisteredAt: time.Now().UTC(),
	}); err != nil {
		if errors.Is(err, ErrDuplicateName) {
			writeJSON(w, http.StatusConflict, errResp{Error: "that username is already taken"})
			return
		}
		log.Printf("identity: insert: %v", err)
		writeJSON(w, http.StatusInternalServerError, errResp{Error: "server error"})
		return
	}
	writeJSON(w, http.StatusOK, registerResp{PlayerID: id})
}

func (s *Service) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.LoginLimit.Allow(ClientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, errResp{Error: "slow down"})
		return
	}
	var req loginReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp{Error: "invalid json"})
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		_ = DummyCompare(req.Password)
		writeJSON(w, http.StatusUnauthorized, errResp{Error: "invalid username or password"})
		return
	}

	p, err := LookupByDisplayName(s.DB, req.Username)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			_ = DummyCompare(req.Password)
			writeJSON(w, http.StatusUnauthorized, errResp{Error: "invalid username or password"})
			return
		}
		log.Printf("identity: login lookup: %v", err)
		writeJSON(w, http.StatusInternalServerError, errResp{Error: "server error"})
		return
	}
	if err := VerifyPassword(p.PasswordHash, req.Password); err != nil {
		writeJSON(w, http.StatusUnauthorized, errResp{Error: "invalid username or password"})
		return
	}
	writeJSON(w, http.StatusOK, loginResp{PlayerID: p.PlayerID, DisplayName: p.DisplayName})
}

func (s *Service) handleVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.VerifyLimit.Allow(ClientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, errResp{Error: "slow down"})
		return
	}
	var req verifyReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp{Error: "invalid json"})
		return
	}
	if len(req.PlayerID) != 32 || len(req.Password) == 0 {
		// Still burn time with a dummy compare to keep response time
		// uniform with a well-formed but missing player_id.
		_ = DummyCompare(req.Password)
		writeJSON(w, http.StatusUnauthorized, errResp{Error: "invalid credentials"})
		return
	}

	p, err := Lookup(s.DB, req.PlayerID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			_ = DummyCompare(req.Password)
			writeJSON(w, http.StatusUnauthorized, errResp{Error: "invalid credentials"})
			return
		}
		log.Printf("identity: lookup: %v", err)
		writeJSON(w, http.StatusInternalServerError, errResp{Error: "server error"})
		return
	}
	if err := VerifyPassword(p.PasswordHash, req.Password); err != nil {
		writeJSON(w, http.StatusUnauthorized, errResp{Error: "invalid credentials"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"display_name": p.DisplayName})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
