package protocol

import (
	"encoding/json"
	"time"
)

const (
	C2SAuth         uint8 = 0x01
	C2SStartRun     uint8 = 0x02
	C2SInput        uint8 = 0x03
	C2SShoot        uint8 = 0x04
	C2SSwitchWeapon uint8 = 0x05
	C2SInteract     uint8 = 0x06
	C2SEnterDoor    uint8 = 0x07
	C2SUseItem      uint8 = 0x08
	C2SShopPurchase uint8 = 0x09
)

const (
	S2CAuthOk       uint8 = 0x81
	S2CRunStarted   uint8 = 0x82
	S2CState        uint8 = 0x83
	S2CRoomLoad     uint8 = 0x84
	S2CRunComplete  uint8 = 0x85
	S2CLeaderboard  uint8 = 0x86
	S2CError        uint8 = 0x87
	S2CDoorUnlocked uint8 = 0x88
	S2CEntityDied   uint8 = 0x89
)

type Envelope struct {
	Type uint8           `json:"type"`
	Seq  uint32          `json:"seq"`
	Ts   int64           `json:"ts"`
	Data json.RawMessage `json:"data"`
}

type S2CEnvelope struct {
	Type     uint8  `json:"type"`
	Seq      uint32 `json:"seq"`
	ServerTs int64  `json:"server_ts"`
	Data     any    `json:"data"`
}

func NewS2C(typ uint8, seq uint32, data any) S2CEnvelope {
	return S2CEnvelope{
		Type:     typ,
		Seq:      seq,
		ServerTs: time.Now().UnixMilli(),
		Data:     data,
	}
}

func DecodeEnvelope(raw []byte) (*Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	return &env, nil
}
