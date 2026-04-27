package main

import (
	"encoding/json"
	"time"
)

// Wire-format constants — kept in sync with server/internal/protocol/envelope.go.
// Duplicated rather than imported so the load tester can live in its own
// module without pulling the server's internal packages.
const (
	c2sAuth     uint8 = 0x01
	c2sStartRun uint8 = 0x02
	c2sInput    uint8 = 0x03
	c2sShoot    uint8 = 0x04
)

const (
	s2cAuthOk      uint8 = 0x81
	s2cRunStarted  uint8 = 0x82
	s2cState       uint8 = 0x83
	s2cRoomLoad    uint8 = 0x84
	s2cRunComplete uint8 = 0x85
	s2cError       uint8 = 0x87
)

type c2sEnvelope struct {
	Type uint8           `json:"type"`
	Seq  uint32          `json:"seq"`
	Ts   int64           `json:"ts"`
	Data json.RawMessage `json:"data"`
}

type s2cEnvelope struct {
	Type     uint8           `json:"type"`
	Seq      uint32          `json:"seq"`
	ServerTs int64           `json:"server_ts"`
	Data     json.RawMessage `json:"data"`
}

type authPacket struct {
	PlayerID string `json:"player_id"`
}

type startRunPacket struct {
	StartTime int64 `json:"start_time"`
}

type inputPacket struct {
	DX float32 `json:"dx"`
	DY float32 `json:"dy"`
}

type shootPacket struct {
	AimAngle float32 `json:"aim_angle"`
}

type errorData struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func encodeC2S(typ uint8, seq uint32, data any) ([]byte, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	env := c2sEnvelope{
		Type: typ,
		Seq:  seq,
		Ts:   time.Now().UnixMilli(),
		Data: payload,
	}
	return json.Marshal(env)
}
