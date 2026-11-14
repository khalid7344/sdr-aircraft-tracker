package tracker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/khalid7344/sdr-aircraft-tracker/internal/adsb"
	"github.com/khalid7344/sdr-aircraft-tracker/internal/ingest"
)

// AircraftState represents the correlated real-time state of a tracked aircraft.
type AircraftState struct {
	ICAO             uint32    `json:"icao"`
	ICAOHex          string    `json:"icao_hex"`
	Callsign         string    `json:"callsign,omitempty"`
	AltitudeFeet     int       `json:"altitude_ft,omitempty"`
	AltitudeValid    bool      `json:"altitude_valid"`
	GroundSpeedKnots float64   `json:"ground_speed_kt,omitempty"`
	TrackDegrees     float64   `json:"track_deg,omitempty"`
	VerticalRateFPM  int       `json:"vertical_rate_fpm,omitempty"`
	VelocityValid    bool      `json:"velocity_valid"`
	Latitude         float64   `json:"latitude,omitempty"`
	Longitude        float64   `json:"longitude,omitempty"`
	PositionValid    bool      `json:"position_valid"`
	LastPositionAt   time.Time `json:"last_position_at,omitempty"`
	MessagesCount    uint64    `json:"messages_count"`
	FirstSeen        time.Time `json:"first_seen"`
	LastSeen         time.Time `json:"last_seen"`

	// Internal CPR buffers (Even = index 0, Odd = index 1)
	EvenLatCPR uint32    `json:"-"`
	EvenLonCPR uint32    `json:"-"`
	EvenTime   time.Time `json:"-"`
	OddLatCPR  uint32    `json:"-"`
	OddLonCPR  uint32    `json:"-"`
	OddTime    time.Time `json:"-"`
}

// Tracker is a thread-safe repository that tracks active aircraft states,
// pairs Even/Odd CPR frames, validates kinematics, and evicts stale records.
type Tracker struct {
	mu       sync.RWMutex
	aircraft map[uint32]*AircraftState
	ttl      time.Duration
	metrics  *ingest.Metrics
}

// NewTracker creates a new Tracker.
func NewTracker(ttl time.Duration, metrics *ingest.Metrics) *Tracker {
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	return &Tracker{
		aircraft: make(map[uint32]*AircraftState),
		ttl:      ttl,
		metrics:  metrics,
	}
}

// Update incorporates a DecodedMessage into the state repository.
func (t *Tracker) Update(msg adsb.DecodedMessage) {
	t.mu.Lock()
	defer t.mu.Unlock()

	state, exists := t.aircraft[msg.ICAO]
	if !exists {
		state = &AircraftState{
			ICAO:      msg.ICAO,
			ICAOHex:   fmt.Sprintf("%06X", msg.ICAO),
			FirstSeen: msg.ReceivedAt,
		}
		t.aircraft[msg.ICAO] = state
	}

	state.LastSeen = msg.ReceivedAt
	state.MessagesCount++

	switch msg.MsgType {
	case adsb.MsgIdentification:
		if msg.Callsign != "" {
			state.Callsign = msg.Callsign
		}

	case adsb.MsgAirborneVel:
		if msg.VelocityValid {
			if !CheckVelocityValid(msg.GroundSpeedKnots) {
				if t.metrics != nil {
					t.metrics.PlausibilityRejects.Add(1)
				}
				return
			}
			state.GroundSpeedKnots = msg.GroundSpeedKnots
			state.TrackDegrees = msg.TrackDegrees
			state.VerticalRateFPM = msg.VerticalRateFPM
			state.VelocityValid = true
		}

	case adsb.MsgAirbornePos:
		if msg.AltitudeValid {
			if !CheckAltitudeValid(msg.AltitudeFeet) {
				if t.metrics != nil {
					t.metrics.PlausibilityRejects.Add(1)
				}
			} else {
				state.AltitudeFeet = msg.AltitudeFeet
				state.AltitudeValid = true
			}
		}

		// Store CPR frame into buffer
		if msg.OddFrame {
			state.OddLatCPR = msg.LatCPR
			state.OddLonCPR = msg.LonCPR
			state.OddTime = msg.ReceivedAt
		} else {
			state.EvenLatCPR = msg.LatCPR
			state.EvenLonCPR = msg.LonCPR
			state.EvenTime = msg.ReceivedAt
		}

		// Check if we have an Even and Odd pair within <= 10 seconds
		if !state.EvenTime.IsZero() && !state.OddTime.IsZero() {
			dt := state.OddTime.Sub(state.EvenTime)
			if dt < 0 {
				dt = -dt
			}
			if dt <= 10*time.Second {
				oddMostRecent := state.OddTime.After(state.EvenTime)
				lat, lon, ok := adsb.DecodeGlobalCPR(
					state.EvenLatCPR, state.EvenLonCPR,
					state.OddLatCPR, state.OddLonCPR,
					oddMostRecent,
				)
				if ok {
					if !CheckCoordinatesValid(lat, lon) {
						if t.metrics != nil {
							t.metrics.PlausibilityRejects.Add(1)
						}
						return
					}

					// Kinematic jump check
					if state.PositionValid && !state.LastPositionAt.IsZero() {
						if !CheckKinematicJumpValid(state.Latitude, state.Longitude, state.LastPositionAt, lat, lon, msg.ReceivedAt) {
							if t.metrics != nil {
								t.metrics.PlausibilityRejects.Add(1)
							}
							return
						}
					}

					state.Latitude = lat
					state.Longitude = lon
					state.PositionValid = true
					state.LastPositionAt = msg.ReceivedAt
					if t.metrics != nil {
						t.metrics.CPRPositionsSolved.Add(1)
					}
				}
			}
		}
	}
}

// Snapshot returns an isolated deep-copy slice of all currently tracked aircraft states.
func (t *Tracker) Snapshot() []AircraftState {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]AircraftState, 0, len(t.aircraft))
	for _, ac := range t.aircraft {
		result = append(result, *ac)
	}
	return result
}

// Get returns a copy of the state for a single aircraft by 24-bit ICAO.
func (t *Tracker) Get(icao uint32) (AircraftState, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	ac, exists := t.aircraft[icao]
	if !exists {
		return AircraftState{}, false
	}
	return *ac, true
}

// Count returns the number of active aircraft.
func (t *Tracker) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.aircraft)
}

// ReapStale evicts aircraft that have not been seen within tracker.ttl.
// Returns the number of evicted aircraft.
func (t *Tracker) ReapStale(now time.Time) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	evicted := 0
	for icao, ac := range t.aircraft {
		if now.Sub(ac.LastSeen) > t.ttl {
			delete(t.aircraft, icao)
			evicted++
		}
	}
	return evicted
}

// StartReaper runs a periodic background eviction ticker until ctx is canceled.
func (t *Tracker) StartReaper(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				t.ReapStale(time.Now())
			case <-ctx.Done():
				return
			}
		}
	}()
}
