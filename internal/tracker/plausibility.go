package tracker

import (
	"math"
	"time"
)

const (
	minAltitudeFeet = -1000
	maxAltitudeFeet = 65000
	maxSpeedKnots   = 1200.0

	// Earth radius in nautical miles (6371 km / 1.852 km/NM)
	earthRadiusNM = 3440.065
)

// CheckCoordinatesValid reports whether latitude and longitude are within legal WGS-84 ranges.
func CheckCoordinatesValid(lat, lon float64) bool {
	return lat >= -90.0 && lat <= 90.0 && lon >= -180.0 && lon <= 180.0
}

// CheckAltitudeValid reports whether barometric altitude is within plausible physical limits (-1000 ft to 65000 ft).
func CheckAltitudeValid(alt int) bool {
	return alt >= minAltitudeFeet && alt <= maxAltitudeFeet
}

// CheckVelocityValid reports whether ground speed is within plausible limits (<= 1200 knots).
func CheckVelocityValid(speedKnots float64) bool {
	return speedKnots >= 0.0 && speedKnots <= maxSpeedKnots
}

// HaversineDistanceNM calculates the great-circle distance between two coordinates in nautical miles.
func HaversineDistanceNM(lat1, lon1, lat2, lon2 float64) float64 {
	rad := math.Pi / 180.0
	phi1 := lat1 * rad
	phi2 := lat2 * rad
	deltaPhi := (lat2 - lat1) * rad
	deltaLambda := (lon2 - lon1) * rad

	sinHalfPhi := math.Sin(deltaPhi / 2.0)
	sinHalfLambda := math.Sin(deltaLambda / 2.0)

	a := sinHalfPhi*sinHalfPhi + math.Cos(phi1)*math.Cos(phi2)*sinHalfLambda*sinHalfLambda
	if a > 1.0 {
		a = 1.0
	}
	c := 2.0 * math.Atan2(math.Sqrt(a), math.Sqrt(1.0-a))
	return earthRadiusNM * c
}

// CheckKinematicJumpValid checks whether moving from (lat1, lon1) at t1 to (lat2, lon2) at t2
// implies a horizontal speed <= 1200 knots over elapsed dt > 0.5s.
func CheckKinematicJumpValid(lat1, lon1 float64, t1 time.Time, lat2, lon2 float64, t2 time.Time) bool {
	dt := t2.Sub(t1).Seconds()
	if dt < 0 {
		dt = -dt
	}
	// For very short intervals (<= 0.5s), do not calculate speed jump to avoid precision artifacts.
	if dt <= 0.5 {
		return true
	}

	distNM := HaversineDistanceNM(lat1, lon1, lat2, lon2)
	speedKnots := (distNM / dt) * 3600.0

	return speedKnots <= maxSpeedKnots
}
