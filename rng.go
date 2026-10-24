// Copyright (c) 2026 Neomantra Corp.

package ntactor

import (
	"math"
	"math/bits"
)

// RandomSource is a pluggable deterministic 64-bit pseudo-random generator.
type RandomSource interface {
	Seed(seed uint64)
	Uint64() uint64
}

// xoshiro256ss implements the deterministic xoshiro256** PRNG seeded via SplitMix64.
type xoshiro256ss struct {
	s [4]uint64
}

func (x *xoshiro256ss) Seed(seed uint64) {
	sm := seed
	for i := range x.s {
		sm += 0x9e3779b97f4a7c15
		z := sm
		z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
		z = (z ^ (z >> 27)) * 0x94d049bb133111eb
		x.s[i] = z ^ (z >> 31)
	}
}

func (x *xoshiro256ss) Uint64() uint64 {
	result := bits.RotateLeft64(x.s[1]*5, 7) * 9
	t := x.s[1] << 17

	x.s[2] ^= x.s[0]
	x.s[3] ^= x.s[1]
	x.s[1] ^= x.s[2]
	x.s[0] ^= x.s[3]

	x.s[2] ^= t
	x.s[3] = bits.RotateLeft64(x.s[3], 45)

	return result
}

// RNG wraps a deterministic RandomSource and provides convenience sampling
// helpers for simulations and behaviors.
type RNG struct {
	seed  uint64
	calls uint64
	src   RandomSource
}

// NewRNG constructs a deterministic RNG seeded with seed.
func NewRNG(seed uint64) *RNG {
	src := &xoshiro256ss{}
	src.Seed(seed)
	return &RNG{
		seed: seed,
		src:  src,
	}
}

// NewRNGWithSource constructs a deterministic RNG backed by a custom RandomSource.
// If src is nil, the default xoshiro256** source is used.
func NewRNGWithSource(seed uint64, src RandomSource) *RNG {
	if src == nil {
		return NewRNG(seed)
	}
	src.Seed(seed)
	return &RNG{
		seed: seed,
		src:  src,
	}
}

// Seed returns the seed most recently used to initialize or reset the RNG.
func (r *RNG) Seed() uint64 {
	return r.seed
}

// Calls returns the total number of Uint64 draws made since initialization or reset.
func (r *RNG) Calls() uint64 {
	return r.calls
}

// Reset re-seeds the underlying source with seed and zeroes the call counter.
func (r *RNG) Reset(seed uint64) {
	r.seed = seed
	r.calls = 0
	r.src.Seed(seed)
}

// Uint64 returns a deterministic pseudo-random uint64.
func (r *RNG) Uint64() uint64 {
	r.calls++
	return r.src.Uint64()
}

// Float64 returns a uniform deterministic float64 in [0.0, 1.0).
func (r *RNG) Float64() float64 {
	return float64(r.Uint64()>>11) * (1.0 / (1 << 53))
}

// FloatRange returns a uniform deterministic float64 in [min, max).
// If max <= min, min is returned without drawing from the source.
func (r *RNG) FloatRange(min, max float64) float64 {
	if max <= min {
		return min
	}
	return min + r.Float64()*(max-min)
}

// Intn returns a uniform deterministic int in [0, n).
// It panics if n <= 0.
func (r *RNG) Intn(n int) int {
	if n <= 0 {
		panic("ntactor: RNG.Intn argument must be > 0")
	}
	un := uint64(n)
	if un&(un-1) == 0 {
		return int(r.Uint64() & (un - 1))
	}
	max := ^uint64(0) - (^uint64(0) % un)
	for {
		v := r.Uint64()
		if v < max {
			return int(v % un)
		}
	}
}

// IntRange returns a uniform deterministic int in [min, max] inclusive.
// If max <= min, min is returned.
func (r *RNG) IntRange(min, max int) int {
	if max <= min {
		return min
	}
	return min + r.Intn(max-min+1)
}

// Bool returns true with the given probability in [0.0, 1.0].
func (r *RNG) Bool(probability float64) bool {
	if probability <= 0 {
		return false
	}
	if probability >= 1 {
		return true
	}
	return r.Float64() < probability
}

// UnitVec2 returns a random unit vector in the XY plane (Z = 0).
func (r *RNG) UnitVec2() Vec3 {
	angle := r.Float64() * 2.0 * math.Pi
	return Vec3{
		X: math.Cos(angle),
		Y: math.Sin(angle),
		Z: 0,
	}
}

// UnitVec3 returns a uniformly distributed 3D unit vector.
func (r *RNG) UnitVec3() Vec3 {
	z := r.FloatRange(-1.0, 1.0)
	theta := r.Float64() * 2.0 * math.Pi
	rad := math.Sqrt(math.Max(0, 1.0-z*z))
	return Vec3{
		X: rad * math.Cos(theta),
		Y: rad * math.Sin(theta),
		Z: z,
	}
}

// Fork derives an independent deterministic child RNG from r's seed and salt
// without advancing r's stream.
func (r *RNG) Fork(salt uint64) *RNG {
	z := r.seed ^ (salt + 0x9e3779b97f4a7c15 + (r.seed << 6) + (r.seed >> 2))
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	derivedSeed := z ^ (z >> 31)
	return NewRNG(derivedSeed)
}
