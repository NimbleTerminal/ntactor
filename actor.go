// Copyright (c) 2026 Neomantra Corp.

package ntactor

// ActorID uniquely identifies an Actor within a World.
// Valid IDs start at 1 and increase monotonically in spawn order.
type ActorID uint64

// InvalidActorID is the zero ActorID and never refers to a spawned Actor.
const InvalidActorID ActorID = 0

// Behavior defines the per-tick decision and update logic for an Actor.
//
// Implementations must derive all timing from dt and world.SimTime(), and all
// randomness from world.RNG() or world.ActorRNG(self.ID). They must never read
// wall-clock time.
type Behavior interface {
	Tick(dt float64, self *Actor, world *World)
}

// BehaviorFunc adapts an ordinary function to the Behavior interface.
type BehaviorFunc func(dt float64, self *Actor, world *World)

// Tick calls f(dt, self, world) if f is non-nil.
func (f BehaviorFunc) Tick(dt float64, self *Actor, world *World) {
	if f != nil {
		f(dt, self, world)
	}
}

// IdleBehavior is a trivial no-op Behavior whose Tick method does nothing.
type IdleBehavior struct{}

// Tick performs no operation.
func (IdleBehavior) Tick(float64, *Actor, *World) {}

// Idle is a shared IdleBehavior instance.
var Idle Behavior = IdleBehavior{}

// Intent represents an abstract action or signal emitted by an Actor during a
// simulation tick. External adapters or game rules can inspect Actor.Intents
// or World.Intents after World.Tick to update visuals, audio, or domain rules.
type Intent struct {
	ActorID ActorID
	Kind    string
	Target  ActorID
	Vector  Vec3
	Value   float64
	Payload any
}

// Event represents an external input or stimulus injected into World.Tick
// (for example: player commands, scripted spawns, or network inputs).
type Event struct {
	Kind    string
	Target  ActorID
	Vector  Vec3
	Value   float64
	Payload any
}

// Actor is an entity in a World owning identity, abstract kinematics, arbitrary
// local state, and a Behavior.
//
// Actors know nothing about meshes, scene graphs, terminal glyphs, or rendering.
type Actor struct {
	ID       ActorID
	Tag      string
	Position Vec3
	Velocity Vec3
	Radius   float64
	Behavior Behavior
	State    any
	Intents  []Intent

	world     *World
	bb        *Blackboard
	mem       *SensoryMemory
	destroyed bool
}

// Blackboard returns the Actor's deterministic key-value memory store,
// allocating it on first access if nil.
func (a *Actor) Blackboard() *Blackboard {
	if a == nil {
		return nil
	}
	if a.bb == nil {
		a.bb = NewBlackboard()
	}
	return a.bb
}

// Memory returns the Actor's deterministic SensoryMemory store, allocating it
// on first access if nil.
func (a *Actor) Memory() *SensoryMemory {
	if a == nil {
		return nil
	}
	if a.mem == nil {
		a.mem = NewSensoryMemory()
	}
	return a.mem
}

// Emit records an Intent for the current tick on both the Actor and its owning
// World. If intent.ActorID is zero, it is filled with a.ID.
func (a *Actor) Emit(intent Intent) {
	if a == nil {
		return
	}
	if intent.ActorID == InvalidActorID {
		intent.ActorID = a.ID
	}
	a.Intents = append(a.Intents, intent)
	if a.world != nil {
		a.world.intents = append(a.world.intents, intent)
	}
}

// EmitKind is a shorthand for emitting an Intent with a kind and optional payload.
func (a *Actor) EmitKind(kind string, payload any) {
	a.Emit(Intent{
		ActorID: a.ID,
		Kind:    kind,
		Payload: payload,
	})
}
