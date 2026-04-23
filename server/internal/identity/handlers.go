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

// Service wires the identity HTTP endpoints against a shared DB + contact key.
type Service struct {
	DB             *sql.DB
	ContactKey     []byte
	RegisterLimit  *RateLimiter
	VerifyLimit    *RateLimiter
}

// NewService builds the service with the spec-mandated rate-limit cooldowns.
func NewService(db *sql.DB, contactKey []byte) *Service {
	return &Service{
		DB:            db,
		ContactKey:    contactKey,
		RegisterLimit: NewRateLimiter(3 * time.Second),
		VerifyLimit:   NewRateLimiter(1 * time.Second),
	}
}

// Register mounts /api/register and /api/verify on mux.
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/register", s.handleRegister)
	mux.HandleFunc("/api/verify", s.handleVerify)
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
	contactEnc, err := EncryptContact(s.ContactKey, req.Contact)
	if err != nil {
		log.Printf("identity: encrypt contact: %v", err)
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
		ContactEnc:   contactEnc,
		RegisteredAt: time.Now().UTC(),
	}); err != nil {
		log.Printf("identity: insert: %v", err)
		writeJSON(w, http.StatusInternalServerError, errResp{Error: "server error"})
		return
	}
	writeJSON(w, http.StatusOK, registerResp{PlayerID: id})
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
