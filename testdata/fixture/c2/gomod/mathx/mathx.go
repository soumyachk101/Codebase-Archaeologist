// Package mathx is a small Go package so the fixture has a real cross-file
// Go dependency edge (calc imports mathx) and a resolved call target.
package mathx

// Clamp returns v bounded to [lo, hi].
func Clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
