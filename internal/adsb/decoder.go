package adsb

import (
	"encoding/hex"
	"math"
	"strings"
	"time"
)

// MessageType identifies the decoded DF17 payload category.
type MessageType uint8

const (
	MsgUnknown        MessageType = iota
	MsgIdentification             // TC 1-4
	MsgAirbornePos                // TC 9-18
	MsgAirborneVel                // TC 19
)

// DecodedMessage holds stateless fields extracted from a single CRC-verified DF17 frame.
type DecodedMessage struct {
	ICAO           uint32
	DownlinkFormat uint8
	TypeCode       uint8
	MsgType        MessageType
	ReceivedAt     time.Time

	// Identification (TC 1-4)
	Callsign string

	// Airborne Position (TC 9-18)
	AltitudeFeet  int
	AltitudeValid bool
	OddFrame      bool
	LatCPR        uint32 // 17-bit encoded latitude (0..131071)
	LonCPR        uint32 // 17-bit encoded longitude (0..131071)

	// Airborne Velocity (TC 19)
	GroundSpeedKnots float64
	TrackDegrees     float64
	VerticalRateFPM  int
	VelocityValid    bool
}

// ICAO AIS 6-bit character lookup table (ICAO Doc 9871).
// Characters 1-26 are 'A'-'Z', 48-57 are '0'-'9', 32 is space (' ').
const aisCharset = "#ABCDEFGHIJKLMNOPQRSTUVWXYZ##### ###############0123456789######"

// DecodeDF17 parses a 14-byte (112-bit) DF17 Mode S frame into a DecodedMessage.
// The frame must have already passed CRC-24 validation.
func DecodeDF17(raw []byte, receivedAt time.Time) (DecodedMessage, bool) {
	if len(raw) != 14 {
		return DecodedMessage{}, false
	}

	df := raw[0] >> 3
	if df != 17 {
		return DecodedMessage{}, false
	}

	icao := (uint32(raw[1]) << 16) | (uint32(raw[2]) << 8) | uint32(raw[3])
	tc := (raw[4] >> 3) & 0x1F

	msg := DecodedMessage{
		ICAO:           icao,
		DownlinkFormat: df,
		TypeCode:       tc,
		ReceivedAt:     receivedAt,
	}

	switch {
	case tc >= 1 && tc <= 4:
		msg.MsgType = MsgIdentification
		msg.Callsign = decodeCallsign(raw[5:11])

	case tc >= 9 && tc <= 18:
		msg.MsgType = MsgAirbornePos
		alt, altValid := decodeAltitude(raw[5], raw[6])
		msg.AltitudeFeet = alt
		msg.AltitudeValid = altValid

		// Odd/Even flag is bit 2 of raw[6] (bit 22 of payload)
		msg.OddFrame = ((raw[6] >> 2) & 1) == 1

		// 17-bit encoded CPR Latitude
		msg.LatCPR = (uint32(raw[6]&0x03) << 15) | (uint32(raw[7]) << 7) | uint32(raw[8]>>1)

		// 17-bit encoded CPR Longitude
		msg.LonCPR = (uint32(raw[8]&0x01) << 16) | (uint32(raw[9]) << 8) | uint32(raw[10])

	case tc == 19:
		msg.MsgType = MsgAirborneVel
		subType := raw[4] & 0x07
		if subType == 1 || subType == 2 {
			speed, track, vr, ok := decodeAirborneVelocity(raw, subType)
			if ok {
				msg.GroundSpeedKnots = speed
				msg.TrackDegrees = track
				msg.VerticalRateFPM = vr
				msg.VelocityValid = true
			}
		}

	default:
		msg.MsgType = MsgUnknown
	}

	return msg, true
}

// FrameStatus indicates the classification of a processed raw frame.
type FrameStatus uint8

const (
	StatusValidDF17 FrameStatus = iota
	StatusNonDF17
	StatusCRCFailed
	StatusInvalidHex
)

// ProcessRawHex classifies a raw hex string, performs CRC-24 validation on DF17 frames,
// and decodes DF17 messages. It correctly handles 56-bit (14 char) short frames,
// non-DF17 112-bit (28 char) frames, corrupted frames, and valid DF17 frames.
func ProcessRawHex(hexStr string, receivedAt time.Time) (DecodedMessage, FrameStatus) {
	if len(hexStr) == 14 {
		return DecodedMessage{}, StatusNonDF17
	}
	if len(hexStr) != 28 {
		return DecodedMessage{}, StatusInvalidHex
	}

	var raw [14]byte
	n, err := hex.Decode(raw[:], []byte(hexStr))
	if err != nil || n != 14 {
		return DecodedMessage{}, StatusInvalidHex
	}

	df := raw[0] >> 3
	if df != 17 {
		return DecodedMessage{}, StatusNonDF17
	}

	if CRC24(raw[:]) != 0 {
		return DecodedMessage{}, StatusCRCFailed
	}

	msg, ok := DecodeDF17(raw[:], receivedAt)
	if !ok {
		return DecodedMessage{}, StatusNonDF17
	}

	return msg, StatusValidDF17
}

// decodeCallsign extracts 8 6-bit AIS characters from a 6-byte slice (48 bits).
func decodeCallsign(b []byte) string {
	if len(b) < 6 {
		return ""
	}
	chars := [8]byte{
		b[0] >> 2,
		((b[0] & 0x03) << 4) | (b[1] >> 4),
		((b[1] & 0x0F) << 2) | (b[2] >> 6),
		b[2] & 0x3F,
		b[3] >> 2,
		((b[3] & 0x03) << 4) | (b[4] >> 4),
		((b[4] & 0x0F) << 2) | (b[5] >> 6),
		b[5] & 0x3F,
	}

	var sb strings.Builder
	sb.Grow(8)
	for _, c := range chars {
		if int(c) < len(aisCharset) {
			ch := aisCharset[c]
			if ch != '#' && ch != '_' {
				sb.WriteByte(ch)
				continue
			}
		}
		sb.WriteByte(' ')
	}
	res := strings.TrimRight(sb.String(), " @")
	return res
}

// decodeAltitude decodes the 12-bit altitude field.
// Supports both 25-ft increments (Q-bit == 1) and Gillham Gray code (Q-bit == 0).
func decodeAltitude(b0, b1 byte) (int, bool) {
	qBit := (b0 & 0x01) == 1
	if qBit {
		high := uint16(b0 >> 1)
		low := uint16(b1 >> 4)
		n := (high << 4) | low
		if n == 0 {
			return 0, false
		}
		alt := int(n)*25 - 1000
		return alt, true
	}

	// Gillham Gray code (Q-bit == 0)
	raw12 := (uint16(b0) << 4) | uint16(b1>>4)
	alt, ok := decodeGillham(raw12)
	return alt, ok
}

// decodeGillham converts Gillham code to 100-foot altitude.
func decodeGillham(raw uint16) (int, bool) {
	// Extract Gillham bits (excluding bit 4 which was Q=0)
	m := uint16(0)
	m |= (raw & 0x800) >> 1 // bit 11 -> bit 10
	m |= (raw & 0x400) >> 1 // bit 10 -> bit 9
	m |= (raw & 0x200) >> 1 // bit 9  -> bit 8
	m |= (raw & 0x100) >> 1 // bit 8  -> bit 7
	m |= (raw & 0x080) >> 1 // bit 7  -> bit 6
	m |= (raw & 0x040) >> 1 // bit 6  -> bit 5
	m |= (raw & 0x020) >> 1 // bit 5  -> bit 4
	m |= (raw & 0x00F)      // bit 3..0 -> bit 3..0

	// Standard Gillham 100-ft decoding:
	c1 := (m >> 4) & 1
	a1 := (m >> 3) & 1
	c2 := (m >> 2) & 1
	a2 := (m >> 1) & 1
	c4 := m & 1
	a4 := (m >> 7) & 1
	b1 := (m >> 6) & 1
	b2 := (m >> 5) & 1
	b4 := (m >> 8) & 1
	d4 := (m >> 9) & 1
	d2 := (m >> 10) & 1

	// Gray to binary conversion for 500-ft increments
	g500 := (d2 << 7) | (d4 << 6) | (a1 << 5) | (a2 << 4) | (a4 << 3) | (b1 << 2) | (b2 << 1) | b4
	b500 := grayToBinary(g500)

	// Gray code for 100-ft increments
	g100 := (c1 << 2) | (c2 << 1) | c4
	b100 := grayToBinary(g100)
	if b100 == 0 || b100 > 4 {
		return 0, false
	}
	if b100 == 5 || b100 == 6 {
		return 0, false
	}

	var offset100 int
	if b500%2 == 1 {
		// Decreasing order
		offset100 = (5 - int(b100)) * 100
	} else {
		// Increasing order
		offset100 = (int(b100) - 1) * 100
	}

	alt := (int(b500)-2)*500 + offset100
	return alt, true
}

func grayToBinary(g uint16) uint16 {
	b := g
	for g > 0 {
		g >>= 1
		b ^= g
	}
	return b
}

// decodeAirborneVelocity parses Subtype 1 (subsonic) and Subtype 2 (supersonic).
func decodeAirborneVelocity(raw []byte, subType byte) (speed float64, track float64, vr int, ok bool) {
	multiplier := 1.0
	if subType == 2 {
		multiplier = 4.0
	}

	// East-West velocity
	sEW := (raw[5] >> 2) & 1
	vEWraw := (uint16(raw[5]&0x03) << 8) | uint16(raw[6])

	// North-South velocity
	sNS := (raw[7] >> 7) & 1
	vNSraw := (uint16(raw[7]&0x7F) << 3) | uint16(raw[8]>>5)

	if vEWraw == 0 || vNSraw == 0 {
		return 0, 0, 0, false
	}

	vEW := (float64(vEWraw) - 1.0) * multiplier
	if sEW == 1 {
		vEW = -vEW
	}

	vNS := (float64(vNSraw) - 1.0) * multiplier
	if sNS == 1 {
		vNS = -vNS
	}

	// Ground speed in knots
	speed = math.Hypot(vEW, vNS)

	// True track heading in degrees (0..359.9)
	trackRad := math.Atan2(vEW, vNS)
	trackDeg := trackRad * (180.0 / math.Pi)
	if trackDeg < 0 {
		trackDeg += 360.0
	}
	track = trackDeg

	// Vertical rate
	sVR := (raw[8] >> 3) & 1
	vrRaw := (uint16(raw[8]&0x07) << 6) | uint16(raw[9]>>2)
	if vrRaw > 0 {
		rate := int(vrRaw-1) * 64
		if sVR == 1 {
			rate = -rate
		}
		vr = rate
	}

	return speed, track, vr, true
}
