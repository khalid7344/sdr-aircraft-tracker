package adsb

// Generator polynomial for Mode S CRC-24:
// G(x) = x^24 + x^23 + x^22 + x^21 + x^20 + x^19 + x^18 + x^17 + x^16 + x^15 + x^14 + x^13 + x^12 + x^10 + x^3 + 1
// Hex representation: 0x1FFF409 (25 bits), lower 24 bits = 0xFFF409.
const ModeSPoly uint32 = 0xFFF409

var crcTable [256]uint32

func init() {
	for i := 0; i < 256; i++ {
		c := uint32(i) << 16
		for j := 0; j < 8; j++ {
			if (c & 0x800000) != 0 {
				c = ((c << 1) ^ ModeSPoly) & 0xFFFFFF
			} else {
				c = (c << 1) & 0xFFFFFF
			}
		}
		crcTable[i] = c
	}
}

// CRC24Bitwise computes the Mode S CRC-24 remainder using pure bitwise division.
// It performs 0 heap allocations and serves as the ground-truth reference implementation.
func CRC24Bitwise(msg []byte) uint32 {
	var rem uint32
	for _, b := range msg {
		for i := 7; i >= 0; i-- {
			bit := uint32((b >> i) & 1)
			high := (rem >> 23) & 1
			rem = ((rem << 1) | bit) & 0xFFFFFF
			if high != 0 {
				rem ^= ModeSPoly
			}
		}
	}
	for i := 0; i < 24; i++ {
		high := (rem >> 23) & 1
		rem = (rem << 1) & 0xFFFFFF
		if high != 0 {
			rem ^= ModeSPoly
		}
	}
	return rem
}

// CRC24 computes the Mode S CRC-24 remainder for a slice of bytes (typically 14 bytes / 112 bits).
// For an uncorrupted DF17 frame with broadcast parity (PI = 0), CRC24(frame) returns 0.
// This function operates with 0 heap allocations and uses a byte-wise lookup table for maximum throughput.
func CRC24(msg []byte) uint32 {
	var rem uint32
	for _, b := range msg {
		idx := ((rem >> 16) ^ uint32(b)) & 0xFF
		rem = ((rem << 8) & 0xFFFFFF) ^ crcTable[idx]
	}
	return rem
}
