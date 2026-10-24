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

// SteerPursue predicts target's future position based on target.Velocity (clamped
// to maxPredictionTime seconds ahead) and returns a desired seek velocity at maxSpeed.
func SteerPursue(self, target *Actor, maxPredictionTime, maxSpeed float64) Vec3 {
	if self == nil || target == nil || maxSpeed <= 0 {
		return Vec3{}
	}
	dist := self.Position.Dist(target.Position)
	speed := self.Velocity.Len()
	if speed < maxSpeed*0.25 {
		speed = maxSpeed
	}
	t := 0.0
	if speed > 1e-9 {
		t = dist / speed
	}
	if maxPredictionTime > 0 && t > maxPredictionTime {
		t = maxPredictionTime
	}
	predicted := target.Position.Add(target.Velocity.Scale(t))
	return SteerSeek(self.Position, predicted, maxSpeed)
}

// SteerEvade predicts threat's future position based on threat.Velocity (clamped
// to maxPredictionTime seconds ahead) and returns a desired flee velocity at maxSpeed.
func SteerEvade(self, threat *Actor, maxPredictionTime, maxSpeed float64) Vec3 {
	if self == nil || threat == nil || maxSpeed <= 0 {
		return Vec3{}
	}
	dist := self.Position.Dist(threat.Position)
	speed := self.Velocity.Len()
	if speed < maxSpeed*0.25 {
		speed = maxSpeed
	}
	t := 0.0
	if speed > 1e-9 {
		t = dist / speed
	}
	if maxPredictionTime > 0 && t > maxPredictionTime {
		t = maxPredictionTime
	}
	predicted := threat.Position.Add(threat.Velocity.Scale(t))
	return SteerFlee(self.Position, predicted, maxSpeed)
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

// SteerAlignment computes a desired velocity aligned with the average heading
// of neighbors at maxSpeed.
func SteerAlignment(self *Actor, neighbors []*Actor, maxSpeed float64) Vec3 {
	if self == nil || len(neighbors) == 0 || maxSpeed <= 0 {
		return Vec3{}
	}
	var sumVel Vec3
	count := 0
	for _, other := range neighbors {
		if other == nil || other.ID == self.ID {
			continue
		}
		sumVel = sumVel.Add(other.Velocity)
		count++
	}
	if count == 0 || sumVel.LenSq() <= 1e-12 {
		return Vec3{}
	}
	return sumVel.Scale(1.0 / float64(count)).Normalize().Scale(maxSpeed)
}

// SteerCohesion computes a desired velocity seeking the center of mass (average
// position) of neighbors at maxSpeed.
func SteerCohesion(self *Actor, neighbors []*Actor, maxSpeed float64) Vec3 {
	if self == nil || len(neighbors) == 0 || maxSpeed <= 0 {
		return Vec3{}
	}
	var sumPos Vec3
	count := 0
	for _, other := range neighbors {
		if other == nil || other.ID == self.ID {
			continue
		}
		sumPos = sumPos.Add(other.Position)
		count++
	}
	if count == 0 {
		return Vec3{}
	}
	center := sumPos.Scale(1.0 / float64(count))
	return SteerSeek(self.Position, center, maxSpeed)
}

// CircleObstacle represents an abstract spherical/circular obstacle for avoidance steering.
type CircleObstacle struct {
	Center Vec3
	Radius float64
}

// SteerAvoidCircles projects self's forward path by lookahead and returns a
// lateral avoidance velocity away from the closest intersecting CircleObstacle.
// If no obstacle intersects the lookahead corridor, the zero Vec3 is returned.
func SteerAvoidCircles(self *Actor, obstacles []CircleObstacle, lookahead, maxSpeed float64) Vec3 {
	if self == nil || len(obstacles) == 0 || lookahead <= 0 || maxSpeed <= 0 {
		return Vec3{}
	}
	fwd := self.Velocity.Normalize()
	if fwd.LenSq() <= 1e-12 {
		return Vec3{}
	}

	bestProj := lookahead + 1.0
	var lateral Vec3
	found := false

	for _, obs := range obstacles {
		toObs := obs.Center.Sub(self.Position)
		proj := toObs.Dot(fwd)
		combinedRadius := obs.Radius + math.Max(0, self.Radius)
		if proj < 0 || proj > lookahead+combinedRadius {
			continue
		}
		closestPoint := self.Position.Add(fwd.Scale(proj))
		offset := closestPoint.Sub(obs.Center)
		distSq := offset.LenSq()
		if distSq < combinedRadius*combinedRadius && proj < bestProj {
			bestProj = proj
			found = true
			if distSq > 1e-12 {
				lateral = offset.Normalize().Scale(maxSpeed)
			} else {
				// Exact head-on alignment: deflect perpendicular in XY plane.
				perp := Vec3{X: -fwd.Y, Y: fwd.X, Z: 0}.Normalize()
				if perp.LenSq() <= 1e-12 {
					perp = Vec3{X: 0, Y: 1, Z: 0}
				}
				lateral = perp.Scale(maxSpeed)
			}
		}
	}

	if !found {
		return Vec3{}
	}
	return lateral
}

// FlockBehavior implements Reynolds Boids flocking (Separation, Alignment, and
// Cohesion) with optional target attraction, threat repulsion, and arena containment.
// It can drive swarms of actors or particle-like boid clouds.
type FlockBehavior struct {
	NeighborRadius   float64
	SeparationRadius float64
	SeparationWeight float64
	AlignmentWeight  float64
	CohesionWeight   float64
	WanderJitter     float64
	MaxSpeed         float64
	MaxForce         float64
	SameTagOnly      bool

	// AttractTag, when non-empty, pulls boids toward the nearest Actor with this Tag.
	AttractTag    string
	AttractWeight float64

	// RepelTag, when non-empty, scatters boids away from Actors with this Tag
	// within RepelRadius.
	RepelTag    string
	RepelRadius float64
	RepelWeight float64

	// BoundsRadius, when > 0, softly steers boids back toward BoundsCenter.
	BoundsCenter Vec3
	BoundsRadius float64

	scratch []*Actor
}

// Tick computes combined flocking forces from nearby Actors and integrates self.Position.
func (f *FlockBehavior) Tick(dt float64, self *Actor, world *World) {
	if f == nil || self == nil || world == nil || dt <= 0 {
		return
	}
	maxSpeed := f.MaxSpeed
	if maxSpeed <= 0 {
		maxSpeed = 6.0
	}
	maxForce := f.MaxForce
	if maxForce <= 0 {
		maxForce = maxSpeed * 3.0
	}
	neighborRad := f.NeighborRadius
	if neighborRad <= 0 {
		neighborRad = 5.0
	}
	sepRad := f.SeparationRadius
	if sepRad <= 0 {
		sepRad = neighborRad * 0.45
	}
	sepWeight := f.SeparationWeight
	if sepWeight == 0 && f.AlignmentWeight == 0 && f.CohesionWeight == 0 {
		sepWeight = 1.6
	}
	alignWeight := f.AlignmentWeight
	if alignWeight == 0 && f.SeparationWeight == 0 && f.CohesionWeight == 0 {
		alignWeight = 1.0
	}
	cohesionWeight := f.CohesionWeight
	if cohesionWeight == 0 && f.SeparationWeight == 0 && f.AlignmentWeight == 0 {
		cohesionWeight = 1.0
	}

	queryRad := neighborRad
	if f.RepelRadius > queryRad {
		queryRad = f.RepelRadius
	}
	f.scratch = world.NeighborsInto(self.ID, queryRad, f.scratch[:0])

	var flockMates []*Actor
	if f.SameTagOnly {
		flockMates = make([]*Actor, 0, len(f.scratch))
		for _, other := range f.scratch {
			if other.Tag == self.Tag && other.Position.DistSq(self.Position) <= neighborRad*neighborRad {
				flockMates = append(flockMates, other)
			}
		}
	} else {
		flockMates = f.scratch
	}

	sep := SteerSeparation(self, flockMates, sepRad, maxSpeed).Scale(sepWeight)
	align := SteerAlignment(self, flockMates, maxSpeed).Scale(alignWeight)
	cohesion := SteerCohesion(self, flockMates, maxSpeed).Scale(cohesionWeight)

	desired := sep.Add(align).Add(cohesion)

	if f.WanderJitter > 0 {
		rng := world.ActorRNG(self.ID)
		wander := SteerWander(self.Velocity, rng, f.WanderJitter, maxSpeed)
		desired = desired.Add(wander.Scale(0.35))
	}

	if f.AttractTag != "" && f.AttractWeight > 0 {
		var closest *Actor
		bestSq := math.MaxFloat64
		for _, a := range world.Actors() {
			if !a.destroyed && a.Tag == f.AttractTag {
				if dSq := self.Position.DistSq(a.Position); dSq < bestSq {
					bestSq = dSq
					closest = a
				}
			}
		}
		if closest != nil {
			desired = desired.Add(SteerSeek(self.Position, closest.Position, maxSpeed).Scale(f.AttractWeight))
		}
	}

	if f.RepelTag != "" && f.RepelRadius > 0 && f.RepelWeight > 0 {
		repelSq := f.RepelRadius * f.RepelRadius
		for _, other := range f.scratch {
			if other.Tag == f.RepelTag && self.Position.DistSq(other.Position) <= repelSq {
				flee := SteerFlee(self.Position, other.Position, maxSpeed)
				desired = desired.Add(flee.Scale(f.RepelWeight))
			}
		}
	}

	if f.BoundsRadius > 0 {
		dist := self.Position.Dist(f.BoundsCenter)
		if dist > f.BoundsRadius {
			pull := SteerSeek(self.Position, f.BoundsCenter, maxSpeed)
			over := (dist - f.BoundsRadius) / f.BoundsRadius
			desired = desired.Add(pull.Scale(1.5 + over*2.0))
		}
	}

	if desired.LenSq() <= 1e-12 {
		rng := world.ActorRNG(self.ID)
		desired = SteerWander(self.Velocity, rng, 0.25, maxSpeed)
	} else {
		desired = desired.ClampLen(maxSpeed)
	}

	ApplySteering(self, desired, maxForce, maxSpeed, dt)
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
