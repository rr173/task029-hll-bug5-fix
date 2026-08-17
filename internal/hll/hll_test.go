package hll

import (
	"fmt"
	"math"
	"testing"
)

func TestNewPrecisionRange(t *testing.T) {
	for p := MinPrecision; p <= MaxPrecision; p++ {
		h, err := New(p)
		if err != nil {
			t.Fatalf("New(%d): unexpected error %v", p, err)
		}
		if got := h.Precision(); got != p {
			t.Errorf("New(%d).Precision() = %d, want %d", p, got, p)
		}
		if got := h.Registers(); got != 1<<p {
			t.Errorf("New(%d).Registers() = %d, want %d", p, got, 1<<p)
		}
	}
	for _, p := range []int{0, 1, 2, 3, 17, 18, 32, -1} {
		if _, err := New(p); err != ErrPrecisionOutOfRange {
			t.Errorf("New(%d): got err=%v, want ErrPrecisionOutOfRange", p, err)
		}
	}
}

func TestErrorBound(t *testing.T) {
	h, _ := New(10)
	want := 1.04 / math.Sqrt(1024)
	if got := h.ErrorBound(); math.Abs(got-want) > 1e-12 {
		t.Errorf("ErrorBound() = %v, want %v", got, want)
	}
}

func TestEmptyEstimateZero(t *testing.T) {
	for _, p := range []int{4, 8, 10, 16} {
		h, _ := New(p)
		if got := h.Estimate(); got != 0 {
			t.Errorf("p=%d empty Estimate() = %v, want 0", p, got)
		}
	}
}

func TestAddIdempotent(t *testing.T) {
	h, _ := New(10)
	h.Add([]byte("abc"))
	e1 := h.Estimate()
	for i := 0; i < 1000; i++ {
		h.Add([]byte("abc"))
	}
	e2 := h.Estimate()
	if e1 != e2 {
		t.Errorf("adding same element changed estimate: %v -> %v", e1, e2)
	}
}

func TestCardinality(t *testing.T) {
	const n = 100000
	h, _ := New(10)
	for i := 0; i < n; i++ {
		h.Add([]byte(fmt.Sprintf("user-%d", i)))
	}
	est := h.Estimate()
	rel := math.Abs(est-n) / float64(n)
	t.Logf("p=10 n=%d estimate=%.0f relerr=%.3f%%", n, est, rel*100)
	if rel > 0.05 {
		t.Errorf("relerr %.3f%% exceeds 5%%", rel*100)
	}
}

func TestMergeMismatch(t *testing.T) {
	a, _ := New(10)
	b, _ := New(11)
	if err := a.Merge(b); err != ErrPrecisionMismatch {
		t.Errorf("Merge different precision: got err=%v, want ErrPrecisionMismatch", err)
	}
}

func TestMergeUnion(t *testing.T) {
	a, _ := New(12)
	b, _ := New(12)
	for i := 1; i <= 50000; i++ {
		a.Add([]byte(fmt.Sprintf("k-%d", i)))
	}
	for i := 30001; i <= 80000; i++ {
		b.Add([]byte(fmt.Sprintf("k-%d", i)))
	}
	if err := a.Merge(b); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	est := a.Estimate()
	rel := math.Abs(est-80000) / 80000
	t.Logf("merge union=80000 estimate=%.0f relerr=%.3f%%", est, rel*100)
	if rel > 0.05 {
		t.Errorf("merge relerr %.3f%% exceeds 5%%", rel*100)
	}
}
