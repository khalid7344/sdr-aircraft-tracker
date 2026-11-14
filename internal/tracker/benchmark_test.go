package tracker

import (
	"testing"
	"time"

	"github.com/khalid7344/sdr-aircraft-tracker/internal/adsb"
	"github.com/khalid7344/sdr-aircraft-tracker/internal/ingest"
)

func BenchmarkTrackerUpdate(b *testing.B) {
	metrics := &ingest.Metrics{}
	tr := NewTracker(60*time.Second, metrics)
	msg := adsb.DecodedMessage{
		ICAO:             0x485020,
		MsgType:          adsb.MsgAirborneVel,
		ReceivedAt:       time.Now(),
		GroundSpeedKnots: 250.0,
		TrackDegrees:     180.0,
		VelocityValid:    true,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		tr.Update(msg)
	}
}

func BenchmarkTrackerSnapshot(b *testing.B) {
	metrics := &ingest.Metrics{}
	tr := NewTracker(60*time.Second, metrics)

	// Populate with 100 aircraft
	for i := 0; i < 100; i++ {
		tr.Update(adsb.DecodedMessage{
			ICAO:             uint32(0x100000 + i),
			MsgType:          adsb.MsgAirborneVel,
			ReceivedAt:       time.Now(),
			GroundSpeedKnots: 300.0,
			VelocityValid:    true,
		})
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = tr.Snapshot()
	}
}
