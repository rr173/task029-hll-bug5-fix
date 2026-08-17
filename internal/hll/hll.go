// Package hll implements a HyperLogLog cardinality estimator with a 64-bit
// hash. It is a self-contained, standard-library-only implementation used by
// the HTTP service to estimate the number of distinct elements in a data
// stream using a fixed-size register array whose size depends only on the
// precision parameter p (registers m = 2^p), not on the cardinality.
package hll

import (
	"errors"
	"hash/fnv"
	"math"
	"math/bits"
)

// Precision bounds. A minimum of 4 keeps at least 16 registers so the bias
// correction constants are well defined; a maximum of 16 caps a single sketch
// at 65536 registers.
const (
	MinPrecision = 4
	MaxPrecision = 16

	// wordBits is the width of the hash in bits.
	wordBits = 64
)

// Sentinel errors returned by this package.
var (
	ErrPrecisionOutOfRange = errors.New("precision out of range [4,16]")
	ErrPrecisionMismatch   = errors.New("precision mismatch")
)

// HLL is a HyperLogLog sketch over a 64-bit hash space.
type HLL struct {
	p         uint8  // precision: number of bits used for register indexing
	m         uint32 // number of registers = 2^p
	registers []uint8
}

// New creates an empty sketch with the given precision p (4 <= p <= 16).
func New(p int) (*HLL, error) {
	if p < MinPrecision || p > MaxPrecision {
		return nil, ErrPrecisionOutOfRange
	}
	m := 1 << p
	return &HLL{
		p:         uint8(p),
		m:         uint32(m),
		registers: make([]uint8, m),
	}, nil
}

// Precision returns the precision p.
func (h *HLL) Precision() int { return int(h.p) }

// Registers returns the number of registers m = 2^p.
func (h *HLL) Registers() int { return int(h.m) }

// ErrorBound returns the theoretical relative standard error 1.04/sqrt(m).
func (h *HLL) ErrorBound() float64 {
	return 1.04 / math.Sqrt(float64(h.m))
}

// Zeros returns the number of registers currently equal to zero.
func (h *HLL) Zeros() int {
	z := 0
	for _, r := range h.registers {
		if r == 0 {
			z++
		}
	}
	return z
}

// Add folds an element into the sketch. Adding the same element more than once
// has no effect on the estimate.
func (h *HLL) Add(data []byte) {
	x := hash64(data)
	idx := x & uint64(h.m-1) // low p bits select the register
	w := x >> h.p              // remaining (64-p) bits form the rank window

	// rho = position of the leftmost 1 bit inside the (64-p)-bit window,
	// counted from its most significant bit, 1-indexed. The top p bits of w
	// (as a 64-bit word) are always zero because of the right shift, so the
	// leading zeros inside the window equal LeadingZeros64(w) - p.
	rho := uint8(bits.LeadingZeros64(w) - int(h.p) + 1)
	if rho > h.registers[idx] {
		h.registers[idx] = rho
	}
}

// Estimate returns the estimated cardinality of the elements seen so far,
// applying the standard small-range (linear counting) and large-range bias
// corrections for a 64-bit hash space.
func (h *HLL) Estimate() float64 {
	m := float64(h.m)
	var sum float64
	zeros := 0
	for _, r := range h.registers {
		sum += math.Pow(2.0, -float64(r))
		if r == 0 {
			zeros++
		}
	}

	alpha := h.alpha()
	e := alpha * m * m / sum

	// Small-range correction: linear counting when the estimate is low and at
	// least one register is still zero.
	if e <= 2.5*m {
		if zeros > 0 {
			return m * math.Log(m/float64(zeros))
		}
		return e
	}

	// Large-range correction for the 64-bit hash space. The threshold is
	// 2^64/30; it is effectively unreachable for realistic inputs but is
	// implemented for completeness.
	twoPow64 := math.Ldexp(1, wordBits)
	if e > twoPow64/30.0 {
		return -twoPow64 * math.Log2(1.0-e/twoPow64)
	}

	return e
}

// Merge folds another sketch into this one by taking the per-register maximum.
// Both sketches must share the same precision.
func (h *HLL) Merge(other *HLL) error {
	if h.p != other.p {
		return ErrPrecisionMismatch
	}
	for i, r := range other.registers {
		if r > h.registers[i] {
			h.registers[i] = r
		}
	}
	return nil
}

// alpha returns the bias correction constant for the register count.
func (h *HLL) alpha() float64 {
	switch h.m {
	case 16:
		return 0.673
	case 32:
		return 0.697
	case 64:
		return 0.709
	default:
		return 0.7213 / (1.0 + 1.079/float64(h.m))
	}
}

// hash64 returns a well-mixed 64-bit hash of data. FNV-1a is deterministic and
// available in the standard library, but its low bits are weak; the splitmix64
// finalizer is a bijective 64-bit mixing function with full avalanche, so every
// output bit depends on every input bit. The result is a deterministic hash
// with good distribution for register indexing and rank computation.
func hash64(data []byte) uint64 {
	h := fnv.New64a()
	h.Write(data)
	return splitmix64(h.Sum64())
}

// splitmix64 is a bijective 64-bit mixing function used to decorrelate the
// FNV-1a output. Each output bit depends on all input bits (full avalanche).
func splitmix64(x uint64) uint64 {
	x += 0x9E3779B97F4A7C15
	x = (x ^ (x >> 30)) * 0xBF58476D1CE4E5B9
	x = (x ^ (x >> 27)) * 0x94D049BB133111EB
	x = x ^ (x >> 31)
	return x
}
