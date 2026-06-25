package compute

import "math"

// HaversineMeters is the great-circle distance in meters between two
// (lat,lng) points. Used by the QR self-check-in presence assessment to put a
// number on "how far from the office did this check-in come from" and "did it
// ping the client's own home address" — the two distances an approving officer
// reads off the green/yellow/red badge.
func HaversineMeters(lat1, lng1, lat2, lng2 float64) float64 {
	const earthRadiusM = 6371000.0
	rad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat := rad(lat2 - lat1)
	dLng := rad(lng2 - lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rad(lat1))*math.Cos(rad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	return earthRadiusM * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
