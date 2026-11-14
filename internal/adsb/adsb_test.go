package adsb

import (
	"encoding/hex"
	"math"
	"testing"
	"time"
)

func TestCRC24ReferenceVectors(t *testing.T) {
	frames := []string{
		"8D4840D6202CC371C32CE0576098", // TC 4 Identification
		"8D40621D58C382D690C8AC2863A7", // TC 11 Airborne Position Even
		"8D40621D58C386435CC412692AD6", // TC 11 Airborne Position Odd
		"8D485020994409940838175B284F", // TC 19 Airborne Velocity
	}

	for _, fHex := range frames {
		raw, err := hex.DecodeString(fHex)
		if err != nil {
			t.Fatalf("hex decode failed: %v", err)
		}
		if rem := CRC24(raw); rem != 0 {
			t.Errorf("frame %s: CRC24 remainder = 0x%06X, want 0x000000", fHex, rem)
		}
		if rem := CRC24Bitwise(raw); rem != 0 {
			t.Errorf("frame %s: CRC24Bitwise remainder = 0x%06X, want 0x000000", fHex, rem)
		}
	}
}

func TestCRCCorruptedRejection(t *testing.T) {
	// Original valid frame
	validHex := "8D4840D6202CC371C32CE0576098"
	raw, err := hex.DecodeString(validHex)
	if err != nil {
		t.Fatal(err)
	}

	// Corrupt each bit one-by-one; every single-bit corruption must be detected (non-zero remainder)
	for byteIdx := 0; byteIdx < len(raw); byteIdx++ {
		for bitIdx := 0; bitIdx < 8; bitIdx++ {
			corrupted := make([]byte, len(raw))
			copy(corrupted, raw)
			corrupted[byteIdx] ^= (1 << bitIdx)

			if rem := CRC24(corrupted); rem == 0 {
				t.Errorf("byte %d bit %d corruption not detected (rem=0)", byteIdx, bitIdx)
			}
		}
	}

	// Specific single-bit flip test vector from PROJECT.md:
	// Flipping lowest bit 8D4840D6202CC371C32CE0576098 -> 8D4840D6202CC371C32CE0576099
	corruptRaw, err := hex.DecodeString("8D4840D6202CC371C32CE0576099")
	if err != nil {
		t.Fatal(err)
	}
	if rem := CRC24(corruptRaw); rem == 0 {
		t.Error("PROJECT.md single-bit flip was not rejected")
	}
}

func TestDecodeIdentification(t *testing.T) {
	// PROJECT.md test vector:
	// Frame: *8D4840D6202CC371C32CE0576098;
	// Expected: Valid CRC-24, ICAO = 0x4840D6 ("4840D6"), TC = 4, Callsign = "KLM1023".
	raw, _ := hex.DecodeString("8D4840D6202CC371C32CE0576098")
	now := time.Now()
	msg, ok := DecodeDF17(raw, now)
	if !ok {
		t.Fatal("DecodeDF17 failed on valid identification frame")
	}

	if msg.ICAO != 0x4840D6 {
		t.Errorf("ICAO = 0x%06X, want 0x4840D6", msg.ICAO)
	}
	if msg.DownlinkFormat != 17 {
		t.Errorf("DF = %d, want 17", msg.DownlinkFormat)
	}
	if msg.TypeCode != 4 {
		t.Errorf("TypeCode = %d, want 4", msg.TypeCode)
	}
	if msg.MsgType != MsgIdentification {
		t.Errorf("MsgType = %v, want MsgIdentification", msg.MsgType)
	}
	if msg.Callsign != "KLM1023" {
		t.Errorf("Callsign = %q, want \"KLM1023\"", msg.Callsign)
	}
}

func TestDecodeAirbornePositionPair(t *testing.T) {
	now := time.Now()

	// Even Frame (F=0): *8D40621D58C382D690C8AC2863A7;
	evenRaw, _ := hex.DecodeString("8D40621D58C382D690C8AC2863A7")
	evenMsg, ok := DecodeDF17(evenRaw, now)
	if !ok {
		t.Fatal("DecodeDF17 failed on Even position frame")
	}
	if evenMsg.ICAO != 0x40621D {
		t.Errorf("Even ICAO = 0x%06X, want 0x40621D", evenMsg.ICAO)
	}
	if !evenMsg.AltitudeValid || evenMsg.AltitudeFeet != 38000 {
		t.Errorf("Even Alt = %d (valid=%v), want 38000", evenMsg.AltitudeFeet, evenMsg.AltitudeValid)
	}
	if evenMsg.OddFrame != false {
		t.Errorf("Even OddFrame = %v, want false", evenMsg.OddFrame)
	}
	if evenMsg.LatCPR != 93000 {
		t.Errorf("Even LatCPR = %d, want 93000", evenMsg.LatCPR)
	}
	if evenMsg.LonCPR != 51372 {
		t.Errorf("Even LonCPR = %d, want 51372", evenMsg.LonCPR)
	}

	// Odd Frame (F=1): *8D40621D58C386435CC412692AD6;
	oddRaw, _ := hex.DecodeString("8D40621D58C386435CC412692AD6")
	oddMsg, ok := DecodeDF17(oddRaw, now)
	if !ok {
		t.Fatal("DecodeDF17 failed on Odd position frame")
	}
	if oddMsg.ICAO != 0x40621D {
		t.Errorf("Odd ICAO = 0x%06X, want 0x40621D", oddMsg.ICAO)
	}
	if !oddMsg.AltitudeValid || oddMsg.AltitudeFeet != 38000 {
		t.Errorf("Odd Alt = %d (valid=%v), want 38000", oddMsg.AltitudeFeet, oddMsg.AltitudeValid)
	}
	if oddMsg.OddFrame != true {
		t.Errorf("Odd OddFrame = %v, want true", oddMsg.OddFrame)
	}
	if oddMsg.LatCPR != 74158 {
		t.Errorf("Odd LatCPR = %d, want 74158", oddMsg.LatCPR)
	}
	if oddMsg.LonCPR != 50194 {
		t.Errorf("Odd LonCPR = %d, want 50194", oddMsg.LonCPR)
	}
}

func TestDecodeAirborneVelocity(t *testing.T) {
	// Frame: *8D485020994409940838175B284F;
	// Expected: Valid CRC-24, ICAO = 0x485020, GroundSpeedKnots ≈ 159.2 kt (± 0.2),
	// TrackDegrees ≈ 182.88° (± 0.1°), VerticalRateFPM = -832.
	raw, _ := hex.DecodeString("8D485020994409940838175B284F")
	now := time.Now()
	msg, ok := DecodeDF17(raw, now)
	if !ok {
		t.Fatal("DecodeDF17 failed on velocity frame")
	}

	if msg.ICAO != 0x485020 {
		t.Errorf("ICAO = 0x%06X, want 0x485020", msg.ICAO)
	}
	if msg.MsgType != MsgAirborneVel {
		t.Errorf("MsgType = %v, want MsgAirborneVel", msg.MsgType)
	}
	if !msg.VelocityValid {
		t.Fatal("VelocityValid = false, want true")
	}

	if math.Abs(msg.GroundSpeedKnots-159.2) > 0.2 {
		t.Errorf("GroundSpeedKnots = %f, want ~159.2", msg.GroundSpeedKnots)
	}
	if math.Abs(msg.TrackDegrees-182.88) > 0.1 {
		t.Errorf("TrackDegrees = %f, want ~182.88", msg.TrackDegrees)
	}
	if msg.VerticalRateFPM != -832 {
		t.Errorf("VerticalRateFPM = %d, want -832", msg.VerticalRateFPM)
	}
}

func TestGlobalCPRVector(t *testing.T) {
	// PROJECT.md Section 7:
	// Even Frame (F=0): LatCPR = 93000, LonCPR = 51372
	// Odd Frame (F=1): LatCPR = 74158, LonCPR = 50194
	// Paired Global CPR Result (oddMostRecent = true):
	// Expected Latitude: 52.25720° ± 0.0005°
	// Expected Longitude: 3.91937° ± 0.0005°
	lat, lon, ok := DecodeGlobalCPR(93000, 51372, 74158, 50194, false)
	if !ok {
		t.Fatal("DecodeGlobalCPR returned ok=false")
	}

	wantLat := 52.25720
	wantLon := 3.91937

	if math.Abs(lat-wantLat) > 0.0005 {
		t.Errorf("Lat = %f, want ~%f (diff=%f)", lat, wantLat, math.Abs(lat-wantLat))
	}
	if math.Abs(lon-wantLon) > 0.0005 {
		t.Errorf("Lon = %f, want ~%f (diff=%f)", lon, wantLon, math.Abs(lon-wantLon))
	}
	t.Logf("Solved Global CPR: Lat=%.5f, Lon=%.5f", lat, lon)
}
func TestProcessRawHexNonDF17AndMalformed(t *testing.T) {
	now := time.Now()

	// Short frame (DF11, 14 hex chars): *02E197B54848A6;
	_, status := ProcessRawHex("02E197B54848A6", now)
	if status != StatusNonDF17 {
		t.Errorf("ProcessRawHex(DF11) status = %v, want StatusNonDF17", status)
	}

	// Corrupted 28-char frame:
	_, status = ProcessRawHex("8D4840D6202CC371C32CE0576099", now)
	if status != StatusCRCFailed {
		t.Errorf("ProcessRawHex(corrupt CRC) status = %v, want StatusCRCFailed", status)
	}

	// Non-hex chars:
	_, status = ProcessRawHex("8D4840D6202CC371C32CE05760ZZ", now)
	if status != StatusInvalidHex {
		t.Errorf("ProcessRawHex(invalid hex) status = %v, want StatusInvalidHex", status)
	}
}
