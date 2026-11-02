// Package calc depends on mathx (resolved cross-file import) and on the
// standard library "math" (external, stays an unresolved reference).
package calc

import (
	"math"

	"example.com/fixture/gomod/mathx"
)

// Normalize scales x into [0, 100] using math.Sqrt then mathx.Clamp.
func Normalize(x float64) float64 {
	return mathx.Clamp(math.Sqrt(x), 0, 100)
}
