// Copyright (c) 2026 Neomantra Corp.

package ntactor

import "math"

// Vec3 is an abstract 3D vector used for Actor position, velocity, and steering.
// Two-dimensional simulations can use X and Y with Z set to 0 (see Vec2).
type Vec3 struct {
	X, Y, Z float64
}

// Vec2 returns a Vec3 in the XY plane with Z set to 0.
func Vec2(x, y float64) Vec3 {
	return Vec3{X: x, Y: y, Z: 0}
}

// Add returns v + other.
func (v Vec3) Add(other Vec3) Vec3 {
	return Vec3{X: v.X + other.X, Y: v.Y + other.Y, Z: v.Z + other.Z}
}

// Sub returns v - other.
func (v Vec3) Sub(other Vec3) Vec3 {
	return Vec3{X: v.X - other.X, Y: v.Y - other.Y, Z: v.Z - other.Z}
}

// Scale returns v * s.
func (v Vec3) Scale(s float64) Vec3 {
	return Vec3{X: v.X * s, Y: v.Y * s, Z: v.Z * s}
}

// Dot returns the dot product of v and other.
func (v Vec3) Dot(other Vec3) float64 {
	return v.X*other.X + v.Y*other.Y + v.Z*other.Z
}

// Cross returns the 3D cross product of v and other.
func (v Vec3) Cross(other Vec3) Vec3 {
	return Vec3{
		X: v.Y*other.Z - v.Z*other.Y,
		Y: v.Z*other.X - v.X*other.Z,
		Z: v.X*other.Y - v.Y*other.X,
	}
}

// LenSq returns the squared Euclidean length of v.
func (v Vec3) LenSq() float64 {
	return v.X*v.X + v.Y*v.Y + v.Z*v.Z
}

// Len returns the Euclidean length of v.
func (v Vec3) Len() float64 {
	return math.Sqrt(v.LenSq())
}

// DistSq returns the squared Euclidean distance between v and other.
func (v Vec3) DistSq(other Vec3) float64 {
	return v.Sub(other).LenSq()
}

// Dist returns the Euclidean distance between v and other.
func (v Vec3) Dist(other Vec3) float64 {
	return v.Sub(other).Len()
}

// Normalize returns a unit vector in the direction of v, or the zero vector if
// v has zero length.
func (v Vec3) Normalize() Vec3 {
	lSq := v.LenSq()
	if lSq <= 1e-18 {
		return Vec3{}
	}
	inv := 1.0 / math.Sqrt(lSq)
	return Vec3{X: v.X * inv, Y: v.Y * inv, Z: v.Z * inv}
}

// ClampLen returns v with its magnitude clamped to at most maxLen.
func (v Vec3) ClampLen(maxLen float64) Vec3 {
	if maxLen <= 0 {
		return Vec3{}
	}
	lSq := v.LenSq()
	if lSq <= maxLen*maxLen {
		return v
	}
	return v.Normalize().Scale(maxLen)
}

// Lerp linearly interpolates between v and target by t in [0, 1].
func (v Vec3) Lerp(target Vec3, t float64) Vec3 {
	return Vec3{
		X: v.X + (target.X-v.X)*t,
		Y: v.Y + (target.Y-v.Y)*t,
		Z: v.Z + (target.Z-v.Z)*t,
	}
}

// Integrate advances self.Position by self.Velocity * dt.
func Integrate(self *Actor, dt float64) {
	if self == nil || dt <= 0 {
		return
	}
	self.Position = self.Position.Add(self.Velocity.Scale(dt))
}

// ApplySteering steers self.Velocity toward desiredVelocity with a maximum
// acceleration of maxForce, clamps velocity magnitude to maxSpeed, and
// integrates self.Position over dt.
func ApplySteering(self *Actor, desiredVelocity Vec3, maxForce, maxSpeed, dt float64) {
	if self == nil || dt <= 0 {
		return
	}
	steer := desiredVelocity.Sub(self.Velocity)
	if maxForce > 0 {
		steer = steer.ClampLen(maxForce * dt)
	}
	self.Velocity = self.Velocity.Add(steer)
	if maxSpeed > 0 {
		self.Velocity = self.Velocity.ClampLen(maxSpeed)
	}
	self.Position = self.Position.Add(self.Velocity.Scale(dt))
}

// SteerSeek returns a desired velocity pointing from pos toward target at maxSpeed.
func SteerSeek(pos, target Vec3, maxSpeed float64) Vec3 {
	if maxSpeed <= 0 {
		return Vec3{}
	}
	return target.Sub(pos).Normalize().Scale(maxSpeed)
}

// SteerFlee returns a desired velocity pointing away from threat at maxSpeed.
func SteerFlee(pos, threat Vec3, maxSpeed float64) Vec3 {
	if maxSpeed <= 0 {
		return Vec3{}
	}
	return pos.Sub(threat).Normalize().Scale(maxSpeed)
}

// SteerArrive returns a desired velocity toward target that smoothly decelerates
// inside slowRadius and reaches zero at target.
func SteerArrive(pos, target Vec3, maxSpeed, slowRadius float64) Vec3 {
	if maxSpeed <= 0 {
		return Vec3{}
	}
	offset := target.Sub(pos)
	dist := offset.Len()
	if dist <= 1e-9 {
		return Vec3{}
	}
	speed := maxSpeed
	if slowRadius > 0 && dist < slowRadius {
		speed = maxSpeed * (dist / slowRadius)
	}
	return offset.Scale(speed / dist)
}

// SteerWander perturbs currentVel using rng in the XY plane by jitter and
// normalizes the result to maxSpeed.
func SteerWander(currentVel Vec3, rng *RNG, jitter, maxSpeed float64) Vec3 {
	if maxSpeed <= 0 || rng == nil {
		return currentVel.ClampLen(maxSpeed)
	}
	dir := currentVel.Normalize()
	if dir.LenSq() <= 1e-9 {
		dir = rng.UnitVec2()
	}
	perturbation := rng.UnitVec2().Scale(jitter)
	return dir.Add(perturbation).Normalize().Scale(maxSpeed)
}

// SteerSeparation computes a repulsive desired velocity away from neighbors
// that are closer than desiredSeparation from self.Position.
func SteerSeparation(self *Actor, neighbors []*Actor, desiredSeparation, maxSpeed float64) Vec3 {
	if self == nil || len(neighbors) == 0 || desiredSeparation <= 0 || maxSpeed <= 0 {
		return Vec3{}
	}
	var push Vec3
	sepSq := desiredSeparation * desiredSeparation
	count := 0
	for _, other := range neighbors {
		if other == nil || other.ID == self.ID {
			continue
		}
		diff := self.Position.Sub(other.Position)
		dSq := diff.LenSq()
		if dSq > 1e-12 && dSq < sepSq {
			dist := math.Sqrt(dSq)
			weight := (desiredSeparation - dist) / desiredSeparation
			push = push.Add(diff.Scale(weight / dist))
			count++
		}
	}
	if count == 0 || push.LenSq() <= 1e-12 {
		return Vec3{}
	}
	return push.Normalize().Scale(maxSpeed)
}

// PatrolBehavior moves an Actor along an ordered sequence of waypoints in a
// loop, advancing to the next waypoint whenever the Actor comes within
// ArrivalRadius of the current waypoint.
type PatrolBehavior struct {
	Waypoints     []Vec3
	Speed         float64
	ArrivalRadius float64
	CurrentIndex  int
}

// Tick advances the Actor toward the current waypoint and integrates Position.
func (p *PatrolBehavior) Tick(dt float64, self *Actor, _ *World) {
	if p == nil || self == nil || len(p.Waypoints) == 0 || dt <= 0 {
		return
	}
	if p.CurrentIndex < 0 || p.CurrentIndex >= len(p.Waypoints) {
		p.CurrentIndex = 0
	}
	radius := p.ArrivalRadius
	if radius <= 0 {
		radius = 0.25
	}
	target := p.Waypoints[p.CurrentIndex]
	if self.Position.Dist(target) <= radius {
		p.CurrentIndex = (p.CurrentIndex + 1) % len(p.Waypoints)
		target = p.Waypoints[p.CurrentIndex]
	}
	self.Velocity = SteerSeek(self.Position, target, p.Speed)
	Integrate(self, dt)
}

// WanderBehavior steers an Actor using deterministic per-actor randomness while
// gently pulling it back toward Center when it strays beyond MaxRadius.
type WanderBehavior struct {
	Speed     float64
	Jitter    float64
	MaxForce  float64
	Center    Vec3
	MaxRadius float64
}

// Tick updates the Actor's velocity using its deterministic ActorRNG and integrates Position.
func (w *WanderBehavior) Tick(dt float64, self *Actor, world *World) {
	if w == nil || self == nil || world == nil || dt <= 0 {
		return
	}
	rng := world.ActorRNG(self.ID)
	desired := SteerWander(self.Velocity, rng, w.Jitter, w.Speed)
	if w.MaxRadius > 0 && self.Position.Dist(w.Center) > w.MaxRadius {
		toCenter := SteerSeek(self.Position, w.Center, w.Speed)
		desired = desired.Lerp(toCenter, 0.5).Normalize().Scale(w.Speed)
	}
	maxForce := w.MaxForce
	if maxForce <= 0 {
		maxForce = w.Speed * 4.0
	}
	ApplySteering(self, desired, maxForce, w.Speed, dt)
}
