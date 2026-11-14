package adsb

import (
	"math"
)

const (
	cprNb = 131072.0 // 2^17
	cprNz = 15
	dLat0 = 360.0 / (4.0 * cprNz)     // 6.0 degrees (Even)
	dLat1 = 360.0 / (4.0*cprNz - 1.0) // 360.0 / 59.0 degrees (Odd)
)

// NL returns the number of longitude zones for a given latitude using ICAO Doc 9871.
// Returns a value between 1 and 59.
func NL(lat float64) int {
	absLat := math.Abs(lat)
	if absLat >= 87.0 {
		return 1
	}
	if absLat == 0.0 {
		return 59
	}

	// ICAO Doc 9871 formula:
	// a = 1 - (1 - cos(pi / (2 * Nz))) / cos^2(pi * lat / 180)
	cosLat := math.Cos(math.Pi * lat / 180.0)
	if cosLat == 0 {
		return 1
	}
	num := 1.0 - math.Cos(math.Pi/(2.0*cprNz))
	den := cosLat * cosLat
	val := 1.0 - (num / den)
	if val < -1.0 || val > 1.0 {
		return 1
	}
	acosVal := math.Acos(val)
	if acosVal == 0 {
		return 1
	}
	nl := int(math.Floor(2.0 * math.Pi / acosVal))
	if nl < 1 {
		return 1
	}
	if nl > 59 {
		return 59
	}
	return nl
}

// cprFloorMod computes (x % m) such that result is always in [0, m).
func cprFloorMod(x, m int) int {
	res := x % m
	if res < 0 {
		res += m
	}
	return res
}

// DecodeGlobalCPR resolves global latitude and longitude from an Even and Odd CPR frame pair.
// Returns (lat, lon, true) on successful resolution, or (0, 0, false) if NL zones differ
// or coordinates are invalid.
func DecodeGlobalCPR(evenLatCPR, evenLonCPR, oddLatCPR, oddLonCPR uint32, oddMostRecent bool) (float64, float64, bool) {
	y0 := float64(evenLatCPR) / cprNb
	y1 := float64(oddLatCPR) / cprNb

	// Latitude zone index j
	j := int(math.Floor(59.0*y0 - 60.0*y1 + 0.5))

	latEven := dLat0 * (float64(cprFloorMod(j, 60)) + y0)
	latOdd := dLat1 * (float64(cprFloorMod(j, 59)) + y1)

	// Southern hemisphere normalization
	if latEven >= 270.0 {
		latEven -= 360.0
	}
	if latOdd >= 270.0 {
		latOdd -= 360.0
	}

	if latEven < -90.0 || latEven > 90.0 || latOdd < -90.0 || latOdd > 90.0 {
		return 0, 0, false
	}

	// FR-05: Check that latitude zones match
	nlEven := NL(latEven)
	nlOdd := NL(latOdd)
	if nlEven != nlOdd {
		return 0, 0, false
	}

	var lat, lon float64
	x0 := float64(evenLonCPR) / cprNb
	x1 := float64(oddLonCPR) / cprNb

	if oddMostRecent {
		lat = latOdd
		nl := nlOdd
		nl1 := nl - 1
		if nl1 < 1 {
			nl1 = 1
		}
		m := int(math.Floor(x0*float64(nl-1) - x1*float64(nl) + 0.5))
		dLon := 360.0 / float64(nl1)
		lon = dLon * (float64(cprFloorMod(m, nl1)) + x1)
	} else {
		lat = latEven
		nl := nlEven
		if nl < 1 {
			nl = 1
		}
		m := int(math.Floor(x0*float64(nl-1) - x1*float64(nl) + 0.5))
		dLon := 360.0 / float64(nl)
		lon = dLon * (float64(cprFloorMod(m, nl)) + x0)
	}

	// Longitude normalization to [-180, +180]
	if lon > 180.0 {
		lon -= 360.0
	}
	if lon < -180.0 {
		lon += 360.0
	}

	return lat, lon, true
}
