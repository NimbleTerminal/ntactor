# ntactor

[![CI](https://github.com/NimbleTerminal/ntactor/actions/workflows/ci.yml/badge.svg)](https://github.com/NimbleTerminal/ntactor/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/nimbleterminal.dev/ntactor.svg)](https://pkg.go.dev/nimbleterminal.dev/ntactor)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Code of Conduct](https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg)](CODE_OF_CONDUCT.md)

`ntactor` is a small, self-contained Go library for classical game and simulation **Actors** and their **Behaviors**.

It is completely abstract and detached from any renderer, scene graph, or 3D engine. Actors and the `World` own only identity, local state, abstract coordinates, and behavior. The same library drives:

- Headless simulations and unit tests
- Pure glyph and terminal games (via [ntcanvas](https://nimbleterminal.dev/ntcanvas) or [Bubble Tea](https://github.com/charmbracelet/bubbletea))
- 3D scene engines and point clouds such as [nt3d](https://nimbleterminal.dev/nt3d) (via an external adapter that `ntactor` never imports)

The root module has **zero external dependencies** and never calls wall-clock APIs (`time.Now`) or global random generators. Given the same random seed and the same sequence of external `(dt, events)` frames, every simulation run is bit-deterministic.

```sh
go get nimbleterminal.dev/ntactor@latest
```

## Quick start

```go
package main

import (
	"fmt"

	actor "nimbleterminal.dev/ntactor"
)

type PatrolState struct {
	Speed float64
}

type myPatrolBehavior struct{}

func (myPatrolBehavior) Tick(dt float64, self *actor.Actor, world *actor.World) {
	st := self.State.(*PatrolState)
	self.Velocity = actor.Vec2(st.Speed, 0)
	actor.Integrate(self, dt)
}

func main() {
	world := actor.NewWorld(actor.Config{
		Seed:       42,
		Perception: actor.NewGridIndex(4.0),
	})

	id := world.Spawn(actor.Actor{
		Behavior: myPatrolBehavior{},
		State:    &PatrolState{Speed: 2.5},
	})

	driver := actor.NewLiveDriver(actor.LiveConfig{
		Seed:      42,
		DefaultDT: 1.0 / 60.0,
		Record:    true,
	})

	for range 60 {
		dt, events := driver.Step()
		world.Tick(dt, events)
		// An external adapter reads world.Actors() and world.Intents() here
		// to update terminal glyphs, nt3d scene nodes, or point clouds.
	}

	a := world.ActorByID(id)
	fmt.Printf("actor %d at (%.2f, %.2f) after %.2fs (hash=0x%016x)\n",
		a.ID, a.Position.X, a.Position.Y, world.SimTime(), world.Hash())
}
```

## What's inside

| File | Types & Helpers | Purpose |
| --- | --- | --- |
| `actor.go` | `ActorID`, `Actor`, `Behavior`, `BehaviorFunc`, `IdleBehavior`, `Intent`, `Event` | Core actor identity, local state, behavior contract, emitted intents, and external events |
| `world.go` | `World`, `Config`, `SpatialIndex` | Single simulation clock (`SimTime`), allocation-light `Tick`, `World.Hash()`, `Spawn`/`Destroy`, ID & tag queries, and radius/cone/LOS perception (`ActorsWithinRadius`, `Neighbors`, `ActorsWithinCone`, `HasLineOfSight`, `NearestActor`) |
| `spatial.go` | `GridIndex`, `NewGridIndex` | Uniform 2D/3D spatial hash grid accelerating neighborhood queries while preserving deterministic `ActorID` order |
| `rng.go` | `RNG`, `RandomSource`, `NewRNG` | Seeded `xoshiro256**` PRNG, pluggable `RandomSource`, and per-actor streams (`World.ActorRNG`) |
| `fsm.go` | `FSM`, `FSMState`, `Transition`, `StateID`, `WildcardState` | Deterministic Finite State Machine with `OnEnter`, `OnTick`, `OnExit`, and ordered conditional transitions |
| `bt.go` | `BehaviorTree`, `BTNode`, `Sequence`, `MemSequence`, `Selector`, `MemSelector`, `Parallel`, `Condition`, `Action`, `Invert`, `Cooldown`, `Timeout` | Stateless and stateful Behavior Tree composites, decorators, and leaves |
| `steering.go` | `Vec3`, `Vec2`, `SteerSeek`, `SteerFlee`, `SteerArrive`, `SteerPursue`, `SteerEvade`, `SteerWander`, `SteerSeparation`, `SteerAlignment`, `SteerCohesion`, `SteerAvoidCircles`, `FlockBehavior`, `PatrolBehavior`, `WanderBehavior` | Abstract 2D/3D vector kinematics, obstacle avoidance, and Reynolds Boids flocking |
| `nav.go` | `NavGraph`, `NodeID`, `FindPath`, `FindPathNodes`, `GridMap`, `FlowField`, `FollowPathBehavior`, `FlowFieldBehavior` | Deterministic A\* pathfinding, line-of-sight path smoothing, weighted occupancy grids, and Dijkstra flow fields |
| `driver_live.go` | `LiveDriver`, `LiveConfig`, `Frame`, `EventLog`, `Driver` | Accumulates wall time (variable clamped `dt` or fixed `FixedDT`), injects live events, and records an `EventLog` |
| `driver_historical.go` | `HistoricalDriver` | Replays a recorded `EventLog` or scripted `[]Frame` sequence for bit-exact historical determinism |
| `codec.go` | `EncodeEventLogJSON`, `DecodeEventLogJSON`, `MarshalEventLogBinary`, `UnmarshalEventLogBinary` | Bounded JSON and compact binary serialization for recorded `EventLog` replays |

## Live vs. Historical replay

External inputs (player commands, network messages, scripted spawns) enter the simulation only as `Event` values passed into `World.Tick(dt, events)`.

`LiveDriver` can record every `(dt, events)` frame it produces into an `EventLog` without taking expensive state snapshots. You can serialize that `EventLog` to JSON or binary bytes, and feed it into `HistoricalDriver` on a fresh `World` initialized with the same `Seed` to reproduce the exact simulation state (`World.Hash()`) at every tick:

```go
// 1. Record a live run:
live := actor.NewLiveDriver(actor.LiveConfig{Seed: 42, Record: true})
live.PushEvent(actor.Event{Kind: "spawn_intruder", Vector: actor.Vec2(4, -2)})
dt, events := live.Step()
liveWorld.Tick(dt, events)

// 2. Replay deterministically in historical mode:
log := live.Log()
replayWorld := actor.NewWorld(actor.Config{Seed: log.Seed})
hist := actor.NewHistoricalDriver(log)
for !hist.Done() {
	dt, events := hist.Step()
	replayWorld.Tick(dt, events)
}
```

## Adapter boundary

`ntactor` never imports a renderer. Instead, your adapter inspects `world.Actors()` and `world.Intents()` after `world.Tick`:

```go
// Terminal glyph adapter (e.g. ntcanvas):
for _, a := range world.Actors() {
	grid.SetText(ntcanvas.Point{X: int(a.Position.X), Y: int(a.Position.Y)}, glyphFor(a))
}

// 3D scene-graph or point-cloud adapter (e.g. nt3d):
for _, a := range world.Actors() {
	scene.SetNodePosition(nodeName(a.ID), a.Position.X, a.Position.Y, a.Position.Z)
}
```

## Examples

The [`examples/`](examples/) directory is a separate module so UI dependencies stay out of `ntactor`'s root `go.mod`:

```sh
task run-replay                # Live run + historical replay check + glyph & 3D node adapters
task run-arena                 # Interactive terminal arena (b: beacon, i: intruder, f: flock wisps, r: replay)
task run-arena -- -headless    # Run arena headlessly and print the dashboard
task run-flock                 # Interactive Boids murmuration / particle-swarm demo (a: attractor, p: predator, r: replay)
task run-flock -- -headless    # Run murmuration headlessly and print density grid + point-cloud stats
task run-nav                   # Interactive A* pathfinding & FlowField maze demo (w: wall, g: goal, s: smooth, c: crowd)
task run-nav -- -headless      # Run navigation demo headlessly and print the maze view
```

See [examples/README.md](examples/README.md) for controls and flags, and [ROADMAP.md](ROADMAP.md) for upcoming features.

## Contributing

Use Go 1.26.8+ and [Go Task](https://taskfile.dev). Run `task ci` to check formatting, module tidiness, race-detector tests, `go vet`, cross-platform builds (`windows`, `linux`, `darwin`, `freebsd`, `js/wasm`), and example binaries.

Contributions and feedback are welcome. Please follow our [Code of Conduct](CODE_OF_CONDUCT.md).

- [Report an issue](https://github.com/NimbleTerminal/ntactor/issues)
- [Submit a pull request](https://github.com/NimbleTerminal/ntactor/pulls)

## License

[MIT License](LICENSE). Copyright (c) 2026 [Neomantra Corp](https://www.neomantra.com).

----
Made with :heart: and :fire: by the team behind [Nimble.Markets](https://nimble.markets).
