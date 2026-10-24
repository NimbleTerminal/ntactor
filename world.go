// Copyright (c) 2026 Neomantra Corp.

package ntactor

import "math"

// SpatialIndex is an optional pluggable spatial index for perception queries.
// When Config.Perception is nil, World uses a deterministic linear slice scan.
type SpatialIndex interface {
	Rebuild(actors []*Actor)
	QueryRadius(center Vec3, radius float64, dst []*Actor) []*Actor
}

// Config configures a new World.
type Config struct {
	// Seed initializes the World's deterministic RNG.
	Seed uint64
	// RNG is an optional custom RandomSource. If nil, the default xoshiro256**
	// source seeded with Seed is used.
	RNG RandomSource
	// Perception is an optional SpatialIndex (such as NewGridIndex) for radius
	// and cone queries. If nil, World scans its ordered actor slice directly.
	Perception SpatialIndex
	// LineOfSight is an optional predicate reporting whether an unobstructed line
	// of sight exists between two positions. When nil, all positions are mutually
	// visible.
	LineOfSight func(from, to Vec3) bool
	// EventHandler is an optional callback invoked in slice order for each
	// external Event at the start of World.Tick before behaviors run.
	EventHandler func(w *World, ev Event)
}

// World owns the simulation clock, deterministic RNG, active Actors, and
// per-tick Events and Intents.
type World struct {
	seed         uint64
	rng          *RNG
	actorRNGs    map[ActorID]*RNG
	simTime      float64
	dt           float64
	tickCount    uint64
	nextID       ActorID
	actors       []*Actor
	indexByID    map[ActorID]int
	ticking      bool
	pendingPurge bool
	spatialDirty bool
	events       []Event
	intents      []Intent
	perception   SpatialIndex
	lineOfSight  func(from, to Vec3) bool
	eventHandler func(w *World, ev Event)
	kindHandlers map[string][]func(w *World, ev Event)
}

// NewWorld constructs a deterministic World from cfg.
func NewWorld(cfg Config) *World {
	return &World{
		seed:         cfg.Seed,
		rng:          NewRNGWithSource(cfg.Seed, cfg.RNG),
		actorRNGs:    make(map[ActorID]*RNG),
		nextID:       1,
		indexByID:    make(map[ActorID]int),
		perception:   cfg.Perception,
		lineOfSight:  cfg.LineOfSight,
		eventHandler: cfg.EventHandler,
	}
}

// Seed returns the initial seed configured on the World.
func (w *World) Seed() uint64 {
	return w.seed
}

// RNG returns the World-owned deterministic RNG.
func (w *World) RNG() *RNG {
	return w.rng
}

// ActorRNG returns a deterministic RNG stream dedicated to the given ActorID,
// derived from the World's seed and id without perturbing the shared World RNG.
func (w *World) ActorRNG(id ActorID) *RNG {
	if rng, ok := w.actorRNGs[id]; ok {
		return rng
	}
	rng := w.rng.Fork(uint64(id))
	w.actorRNGs[id] = rng
	return rng
}

// SimTime returns the current accumulated simulation time in seconds.
func (w *World) SimTime() float64 {
	return w.simTime
}

// DT returns the delta time of the current or most recently completed Tick.
func (w *World) DT() float64 {
	return w.dt
}

// TickCount returns the number of simulation ticks executed since World creation.
func (w *World) TickCount() uint64 {
	return w.tickCount
}

// HasLineOfSight reports whether an unobstructed line of sight exists between
// from and to according to Config.LineOfSight. If no LineOfSight callback was
// configured, it always returns true.
func (w *World) HasLineOfSight(from, to Vec3) bool {
	if w.lineOfSight == nil {
		return true
	}
	return w.lineOfSight(from, to)
}

// OnEvent registers a deterministic handler for external Events of the given Kind.
// Handlers execute in registration order at the start of World.Tick before
// actor behaviors run.
func (w *World) OnEvent(kind string, fn func(w *World, ev Event)) {
	if fn == nil {
		return
	}
	if w.kindHandlers == nil {
		w.kindHandlers = make(map[string][]func(w *World, ev Event))
	}
	w.kindHandlers[kind] = append(w.kindHandlers[kind], fn)
}

// Spawn adds an Actor to the World, assigns it a unique deterministic ActorID,
// and returns that ID.
func (w *World) Spawn(a Actor) ActorID {
	id := w.nextID
	w.nextID++

	a.ID = id
	a.world = w
	a.destroyed = false
	if len(a.Intents) > 0 {
		a.Intents = a.Intents[:0]
	}

	actorPtr := &a
	w.indexByID[id] = len(w.actors)
	w.actors = append(w.actors, actorPtr)
	w.spatialDirty = true
	return id
}

// Destroy removes the Actor with the given ID from the World.
// It returns true if the Actor existed and was removed.
func (w *World) Destroy(id ActorID) bool {
	idx, ok := w.indexByID[id]
	if !ok {
		return false
	}
	target := w.actors[idx]
	if target.destroyed {
		return false
	}
	target.destroyed = true
	target.world = nil
	delete(w.indexByID, id)
	delete(w.actorRNGs, id)
	w.spatialDirty = true

	if w.ticking {
		w.pendingPurge = true
		return true
	}

	w.removeAt(idx)
	return true
}

func (w *World) removeAt(idx int) {
	last := len(w.actors) - 1
	copy(w.actors[idx:], w.actors[idx+1:])
	w.actors[last] = nil
	w.actors = w.actors[:last]
	for i := idx; i < len(w.actors); i++ {
		w.indexByID[w.actors[i].ID] = i
	}
}

func (w *World) compactDestroyed() {
	write := 0
	for _, a := range w.actors {
		if !a.destroyed {
			w.actors[write] = a
			w.indexByID[a.ID] = write
			write++
		}
	}
	for i := write; i < len(w.actors); i++ {
		w.actors[i] = nil
	}
	w.actors = w.actors[:write]
	w.pendingPurge = false
}

// Actor looks up an active Actor by ID.
func (w *World) Actor(id ActorID) (*Actor, bool) {
	idx, ok := w.indexByID[id]
	if !ok {
		return nil, false
	}
	a := w.actors[idx]
	if a == nil || a.destroyed {
		return nil, false
	}
	return a, true
}

// ActorByID returns the active Actor with the given ID, or nil if not found.
func (w *World) ActorByID(id ActorID) *Actor {
	a, _ := w.Actor(id)
	return a
}

// Count returns the number of active Actors in the World.
func (w *World) Count() int {
	return len(w.indexByID)
}

// Actors returns all active Actors in deterministic ActorID spawn order.
func (w *World) Actors() []*Actor {
	if !w.pendingPurge {
		return w.actors
	}
	out := make([]*Actor, 0, len(w.indexByID))
	for _, a := range w.actors {
		if !a.destroyed {
			out = append(out, a)
		}
	}
	return out
}

// Events returns the external Events injected into the current or most recent Tick.
func (w *World) Events() []Event {
	return w.events
}

// EventsFor appends to dst and returns all Events from the current Tick whose
// Target matches id or is InvalidActorID (broadcast).
func (w *World) EventsFor(id ActorID, dst []Event) []Event {
	for _, ev := range w.events {
		if ev.Target == id || ev.Target == InvalidActorID {
			dst = append(dst, ev)
		}
	}
	return dst
}

// Intents returns all Intents emitted across all Actors during the current or
// most recent Tick, in deterministic execution order.
func (w *World) Intents() []Intent {
	return w.intents
}

func (w *World) ensureSpatialIndex() {
	if w.perception != nil && w.spatialDirty {
		w.perception.Rebuild(w.actors)
		w.spatialDirty = false
	}
}

// Tick advances the simulation clock by dt (clamped to >= 0), dispatches
// injected external events, and updates all active Actors in deterministic ID order.
func (w *World) Tick(dt float64, events []Event) {
	if dt < 0 || math.IsNaN(dt) || math.IsInf(dt, 0) {
		dt = 0
	}
	w.dt = dt
	w.simTime += dt
	w.tickCount++

	w.events = append(w.events[:0], events...)
	w.intents = w.intents[:0]

	for _, a := range w.actors {
		if !a.destroyed && len(a.Intents) > 0 {
			a.Intents = a.Intents[:0]
		}
	}

	for _, ev := range w.events {
		if w.eventHandler != nil {
			w.eventHandler(w, ev)
		}
		if len(w.kindHandlers) > 0 {
			for _, fn := range w.kindHandlers[ev.Kind] {
				fn(w, ev)
			}
		}
	}

	if w.pendingPurge {
		w.compactDestroyed()
	}

	if w.perception != nil {
		w.perception.Rebuild(w.actors)
		w.spatialDirty = false
	}

	w.ticking = true
	activeCount := len(w.actors)
	for i := 0; i < activeCount; i++ {
		a := w.actors[i]
		if !a.destroyed && a.Behavior != nil {
			a.Behavior.Tick(dt, a, w)
		}
	}
	w.ticking = false
	w.spatialDirty = true

	if w.pendingPurge {
		w.compactDestroyed()
	}
}

// ActorsWithinRadius returns all active Actors whose Position is within radius
// (inclusive) of center, in deterministic ActorID order.
func (w *World) ActorsWithinRadius(center Vec3, radius float64) []*Actor {
	return w.ActorsWithinRadiusInto(center, radius, nil)
}

// ActorsWithinRadiusInto appends to dst and returns all active Actors whose
// Position is within radius (inclusive) of center.
func (w *World) ActorsWithinRadiusInto(center Vec3, radius float64, dst []*Actor) []*Actor {
	if radius < 0 {
		return dst
	}
	if w.perception != nil {
		w.ensureSpatialIndex()
		return w.perception.QueryRadius(center, radius, dst)
	}
	rSq := radius * radius
	for _, a := range w.actors {
		if !a.destroyed && a.Position.DistSq(center) <= rSq {
			dst = append(dst, a)
		}
	}
	return dst
}

// Neighbors returns all active Actors within radius of selfID's Position,
// excluding selfID itself, in deterministic ActorID order.
func (w *World) Neighbors(selfID ActorID, radius float64) []*Actor {
	return w.NeighborsInto(selfID, radius, nil)
}

// NeighborsInto appends to dst and returns all active Actors within radius of
// selfID's Position, excluding selfID itself.
func (w *World) NeighborsInto(selfID ActorID, radius float64, dst []*Actor) []*Actor {
	self, ok := w.Actor(selfID)
	if !ok || radius < 0 {
		return dst
	}
	if w.perception != nil {
		w.ensureSpatialIndex()
		start := len(dst)
		dst = w.perception.QueryRadius(self.Position, radius, dst)
		write := start
		for i := start; i < len(dst); i++ {
			if dst[i].ID != selfID {
				dst[write] = dst[i]
				write++
			}
		}
		for i := write; i < len(dst); i++ {
			dst[i] = nil
		}
		return dst[:write]
	}
	rSq := radius * radius
	for _, a := range w.actors {
		if !a.destroyed && a.ID != selfID && a.Position.DistSq(self.Position) <= rSq {
			dst = append(dst, a)
		}
	}
	return dst
}

// ActorsWithinCone returns all active Actors within radius of origin whose
// direction from origin lies within halfAngleRad (in radians) of forward and
// that satisfy HasLineOfSight(origin, actor.Position), in deterministic ActorID order.
func (w *World) ActorsWithinCone(origin, forward Vec3, radius, halfAngleRad float64) []*Actor {
	return w.ActorsWithinConeInto(origin, forward, radius, halfAngleRad, nil)
}

// ActorsWithinConeInto appends to dst and returns all active Actors within the
// specified vision cone and line-of-sight check.
func (w *World) ActorsWithinConeInto(origin, forward Vec3, radius, halfAngleRad float64, dst []*Actor) []*Actor {
	if radius < 0 || halfAngleRad < 0 {
		return dst
	}
	fwd := forward.Normalize()
	if fwd.LenSq() == 0 {
		return dst
	}
	cosHalf := math.Cos(math.Min(math.Pi, halfAngleRad))

	start := len(dst)
	dst = w.ActorsWithinRadiusInto(origin, radius, dst)
	write := start
	for i := start; i < len(dst); i++ {
		a := dst[i]
		offset := a.Position.Sub(origin)
		dLen := offset.Len()
		inCone := dLen <= 1e-9 || offset.Scale(1.0/dLen).Dot(fwd) >= cosHalf-1e-12
		if inCone && w.HasLineOfSight(origin, a.Position) {
			dst[write] = a
			write++
		}
	}
	for i := write; i < len(dst); i++ {
		dst[i] = nil
	}
	return dst[:write]
}

// NearestActor returns the closest active Actor to center within maxRadius
// (excluding excludeID), along with its distance. If none is within maxRadius,
// it returns (nil, 0). Ties are broken deterministically by lowest ActorID.
func (w *World) NearestActor(center Vec3, maxRadius float64, excludeID ActorID) (*Actor, float64) {
	if maxRadius < 0 {
		return nil, 0
	}
	maxSq := maxRadius * maxRadius
	var best *Actor
	bestSq := maxSq
	for _, a := range w.actors {
		if a.destroyed || a.ID == excludeID {
			continue
		}
		dSq := a.Position.DistSq(center)
		if dSq <= maxSq && (best == nil || dSq < bestSq) {
			best = a
			bestSq = dSq
		}
	}
	if best == nil {
		return nil, 0
	}
	return best, math.Sqrt(bestSq)
}

// FindByTag returns all active Actors with the given Tag in deterministic ActorID order.
func (w *World) FindByTag(tag string) []*Actor {
	var out []*Actor
	for _, a := range w.actors {
		if !a.destroyed && a.Tag == tag {
			out = append(out, a)
		}
	}
	return out
}

// Hash computes a deterministic 64-bit FNV-1a digest over the World's seed,
// simulation clock, tick count, RNG draw count, and all active Actors' identity
// and kinematics in deterministic ActorID order.
func (w *World) Hash() uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	mixUint64 := func(v uint64) {
		for range 8 {
			h ^= v & 0xff
			h *= prime64
			v >>= 8
		}
	}
	mixString := func(s string) {
		mixUint64(uint64(len(s)))
		for i := 0; i < len(s); i++ {
			h ^= uint64(s[i])
			h *= prime64
		}
	}

	mixUint64(w.seed)
	mixUint64(math.Float64bits(w.simTime))
	mixUint64(w.tickCount)
	mixUint64(w.rng.Calls())

	for _, a := range w.actors {
		if a == nil || a.destroyed {
			continue
		}
		mixUint64(uint64(a.ID))
		mixString(a.Tag)
		mixUint64(math.Float64bits(a.Position.X))
		mixUint64(math.Float64bits(a.Position.Y))
		mixUint64(math.Float64bits(a.Position.Z))
		mixUint64(math.Float64bits(a.Velocity.X))
		mixUint64(math.Float64bits(a.Velocity.Y))
		mixUint64(math.Float64bits(a.Velocity.Z))
		mixUint64(math.Float64bits(a.Radius))
	}
	return h
}
