package tracker

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/khalid7344/sdr-aircraft-tracker/internal/adsb"
	"github.com/khalid7344/sdr-aircraft-tracker/internal/ingest"
)

func TestTrackerPositionPairing(t *testing.T) {
	metrics := &ingest.Metrics{}
	tr := NewTracker(60*time.Second, metrics)

	t0 := time.Now()
	// Even frame for 0x40621D
	tr.Update(adsb.DecodedMessage{
		ICAO:          0x40621D,
		MsgType:       adsb.MsgAirbornePos,
		ReceivedAt:    t0,
		AltitudeFeet:  38000,
		AltitudeValid: true,
		OddFrame:      false,
		LatCPR:        93000,
		LonCPR:        51372,
	})

	state, ok := tr.Get(0x40621D)
	if !ok {
		t.Fatal("aircraft not found in tracker")
	}
	if state.PositionValid {
		t.Error("PositionValid = true after single frame, want false")
	}

	// Odd frame received 2 seconds later (within 10s window)
	t1 := t0.Add(2 * time.Second)
	tr.Update(adsb.DecodedMessage{
		ICAO:          0x40621D,
		MsgType:       adsb.MsgAirbornePos,
		ReceivedAt:    t1,
		AltitudeFeet:  38000,
		AltitudeValid: true,
		OddFrame:      true,
		LatCPR:        74158,
		LonCPR:        50194,
	})

	state, ok = tr.Get(0x40621D)
	if !ok {
		t.Fatal("aircraft not found in tracker")
	}
	if !state.PositionValid {
		t.Error("PositionValid = false after Even/Odd pair within 2s, want true")
	}
	if math.Abs(state.Latitude-52.26578) > 0.001 {
		t.Errorf("Latitude = %f, want ~52.26578", state.Latitude)
	}
	if math.Abs(state.Longitude-3.93891) > 0.001 {
		t.Errorf("Longitude = %f, want ~3.93891", state.Longitude)
	}
	if metrics.CPRPositionsSolved.Load() != 1 {
		t.Errorf("CPRPositionsSolved = %d, want 1", metrics.CPRPositionsSolved.Load())
	}
}

func TestTrackerPairingTimeout(t *testing.T) {
	metrics := &ingest.Metrics{}
	tr := NewTracker(60*time.Second, metrics)

	t0 := time.Now()
	// Even frame
	tr.Update(adsb.DecodedMessage{
		ICAO:          0x40621D,
		MsgType:       adsb.MsgAirbornePos,
		ReceivedAt:    t0,
		AltitudeFeet:  38000,
		AltitudeValid: true,
		OddFrame:      false,
		LatCPR:        93000,
		LonCPR:        51372,
	})

	// Odd frame received 15 seconds later (> 10s window)
	t1 := t0.Add(15 * time.Second)
	tr.Update(adsb.DecodedMessage{
		ICAO:          0x40621D,
		MsgType:       adsb.MsgAirbornePos,
		ReceivedAt:    t1,
		AltitudeFeet:  38000,
		AltitudeValid: true,
		OddFrame:      true,
		LatCPR:        74158,
		LonCPR:        50194,
	})

	state, _ := tr.Get(0x40621D)
	if state.PositionValid {
		t.Error("PositionValid = true when frames separated by 15s, want false")
	}
	if metrics.CPRPositionsSolved.Load() != 0 {
		t.Errorf("CPRPositionsSolved = %d, want 0", metrics.CPRPositionsSolved.Load())
	}
}

func TestPlausibilityFilters(t *testing.T) {
	metrics := &ingest.Metrics{}
	tr := NewTracker(60*time.Second, metrics)

	// 1. Implausible altitude (> 65000 ft)
	tr.Update(adsb.DecodedMessage{
		ICAO:          0x123456,
		MsgType:       adsb.MsgAirbornePos,
		ReceivedAt:    time.Now(),
		AltitudeFeet:  80000,
		AltitudeValid: true,
	})
	state, _ := tr.Get(0x123456)
	if state.AltitudeValid {
		t.Error("AltitudeValid = true for 80,000 ft, want false")
	}

	// 2. Implausible speed (> 1200 knots)
	tr.Update(adsb.DecodedMessage{
		ICAO:             0x123456,
		MsgType:          adsb.MsgAirborneVel,
		ReceivedAt:       time.Now(),
		GroundSpeedKnots: 1500.0,
		VelocityValid:    true,
	})
	state, _ = tr.Get(0x123456)
	if state.VelocityValid {
		t.Error("VelocityValid = true for 1500 kt, want false")
	}

	if metrics.PlausibilityRejects.Load() < 2 {
		t.Errorf("PlausibilityRejects = %d, want >= 2", metrics.PlausibilityRejects.Load())
	}
}

func TestKinematicJumpRejection(t *testing.T) {
	metrics := &ingest.Metrics{}
	tr := NewTracker(60*time.Second, metrics)

	t0 := time.Now()
	// Initial position: London (51.5074, -0.1278)
	state := &AircraftState{
		ICAO:           0x123456,
		Latitude:       51.5074,
		Longitude:      -0.1278,
		PositionValid:  true,
		LastPositionAt: t0,
	}
	tr.aircraft[0x123456] = state

	// Simulated sudden jump to New York (40.7128, -74.0060) in 2 seconds (~3000 NM in 2s => ~5,000,000 kt)
	valid := CheckKinematicJumpValid(state.Latitude, state.Longitude, t0, 40.7128, -74.0060, t0.Add(2*time.Second))
	if valid {
		t.Error("CheckKinematicJumpValid accepted 3000 NM jump in 2 seconds")
	}
}

func TestTTLEviction(t *testing.T) {
	metrics := &ingest.Metrics{}
	tr := NewTracker(10*time.Second, metrics)

	now := time.Now()
	// Stale aircraft (seen 20s ago)
	tr.aircraft[0xAAAAAA] = &AircraftState{
		ICAO:     0xAAAAAA,
		LastSeen: now.Add(-20 * time.Second),
	}
	// Active aircraft (seen 2s ago)
	tr.aircraft[0xBBBBBB] = &AircraftState{
		ICAO:     0xBBBBBB,
		LastSeen: now.Add(-2 * time.Second),
	}

	evicted := tr.ReapStale(now)
	if evicted != 1 {
		t.Errorf("ReapStale evicted = %d, want 1", evicted)
	}
	if _, ok := tr.Get(0xAAAAAA); ok {
		t.Error("stale aircraft 0xAAAAAA still exists")
	}
	if _, ok := tr.Get(0xBBBBBB); !ok {
		t.Error("active aircraft 0xBBBBBB was incorrectly evicted")
	}
}

func TestConcurrentAccessRaceFree(t *testing.T) {
	metrics := &ingest.Metrics{}
	tr := NewTracker(30*time.Second, metrics)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup

	// 5 concurrent writers
	for w := 0; w < 5; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			baseICAO := uint32(workerID * 100)
			for i := 0; ; i++ {
				select {
				case <-ctx.Done():
					return
				default:
					icao := baseICAO + uint32(i%20)
					tr.Update(adsb.DecodedMessage{
						ICAO:             icao,
						MsgType:          adsb.MsgAirborneVel,
						ReceivedAt:       time.Now(),
						GroundSpeedKnots: 250.0 + float64(i%50),
						TrackDegrees:     180.0,
						VelocityValid:    true,
					})
				}
			}
		}(w)
	}

	// 3 concurrent snapshot readers
	for r := 0; r < 3; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					snap := tr.Snapshot()
					_ = len(snap)
					time.Sleep(1 * time.Millisecond)
				}
			}
		}()
	}

	wg.Wait()
}
