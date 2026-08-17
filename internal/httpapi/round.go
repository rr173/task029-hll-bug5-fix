package httpapi

import "math"

// mathRound rounds to nearest, ties away from zero. (math.Round rounds half to
// even in Go, which is fine, but we name it explicitly to document intent.)
func mathRound(f float64) float64 {
	return math.Round(f)
}
