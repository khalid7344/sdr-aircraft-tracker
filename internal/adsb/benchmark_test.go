package adsb

import (
	"encoding/hex"
	"testing"
	"time"
)

func BenchmarkCRC24(b *testing.B) {
	raw, _ := hex.DecodeString("8D4840D6202CC371C32CE0576098")
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = CRC24(raw)
	}
}

func BenchmarkCRC24Bitwise(b *testing.B) {
	raw, _ := hex.DecodeString("8D4840D6202CC371C32CE0576098")
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = CRC24Bitwise(raw)
	}
}

func BenchmarkDecodeDF17(b *testing.B) {
	raw, _ := hex.DecodeString("8D40621D58C382D690C8AC2863A7")
	now := time.Now()
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = DecodeDF17(raw, now)
	}
}

func BenchmarkProcessRawHex(b *testing.B) {
	hexStr := "8D40621D58C382D690C8AC2863A7"
	now := time.Now()
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = ProcessRawHex(hexStr, now)
	}
}

func BenchmarkGlobalCPR(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _, _ = DecodeGlobalCPR(93000, 51372, 74158, 50194, false)
	}
}
