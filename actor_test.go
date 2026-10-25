// Copyright (c) 2026 Neomantra Corp.

package ntactor

import (
	"bytes"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"strings"
	"testing"
)

type guardState struct {
	Energy     float64
	Alerts     int
	LastTarget ActorID
}

func buildTestSimulationWorld(seed uint64) *World {
	world := NewWorld(Config{Seed: seed})

	world.OnEvent("spawn_intruder", func(w *World, ev Event) {
		w.Spawn(Actor{
			Tag:      "intruder",
			Position: ev.Vector,
			Radius:   1.0,
			Behavior: &WanderBehavior{
				Speed:     ev.Value,
				Jitter:    0.35,
				Center:    Vec2(0, 0),
				MaxRadius: 15.0,
			},
			State: &guardState{Energy: 100},
		})
	})

	world.OnEvent("remove_actor", func(w *World, ev Event) {
		if ev.Target != InvalidActorID {
			w.Destroy(ev.Target)
		}
	})

	// Spawn a patrolling guard driven by an FSM.
	patrol := &PatrolBehavior{
		Waypoints: []Vec3{
			Vec2(-5, -5),
			Vec2(5, -5),
			Vec2(5, 5),
			Vec2(-5, 5),
		},
		Speed:         3.0,
		ArrivalRadius: 0.5,
	}

	fsm := NewFSM("patrol")
	fsm.AddState("patrol", FSMState{
		OnEnter: func(self *Actor, _ *World) {
			self.EmitKind("state_enter", "patrol")
		},
		OnTick: func(dt float64, self *Actor, w *World) {
			patrol.Tick(dt, self, w)
			st := self.State.(*guardState)
			st.Energy = math.Min(100, st.Energy+dt*2.0)
		},
	})
	fsm.AddState("pursue", FSMState{
		OnEnter: func(self *Actor, _ *World) {
			st := self.State.(*guardState)
			st.Alerts++
			self.EmitKind("alert", st.Alerts)
		},
		OnTick: func(dt float64, self *Actor, w *World) {
			st := self.State.(*guardState)
			target, _ := w.NearestActor(self.Position, 8.0, self.ID)
			if target != nil {
				st.LastTarget = target.ID
				desired := SteerSeek(self.Position, target.Position, 4.5)
				ApplySteering(self, desired, 12.0, 4.5, dt)
			}
			st.Energy -= dt * 15.0
		},
	})
	fsm.AddState("rest", FSMState{
		OnTick: func(dt float64, self *Actor, _ *World) {
			self.Velocity = Vec3{}
			st := self.State.(*guardState)
			st.Energy += dt * 25.0
		},
	})

	fsm.AddTransition("patrol", "pursue", func(self *Actor, w *World) bool {
		near, _ := w.NearestActor(self.Position, 6.0, self.ID)
		return near != nil && near.Tag == "intruder"
	})
	fsm.AddTransition("pursue", "rest", func(self *Actor, _ *World) bool {
		return self.State.(*guardState).Energy <= 20.0
	})
	fsm.AddTransition("pursue", "patrol", func(self *Actor, w *World) bool {
		near, _ := w.NearestActor(self.Position, 8.0, self.ID)
		return near == nil
	})
	fsm.AddTransition("rest", "patrol", func(self *Actor, _ *World) bool {
		return self.State.(*guardState).Energy >= 80.0
	})

	world.Spawn(Actor{
		Tag:      "guard",
		Position: Vec2(-5, -5),
		Radius:   1.5,
		Behavior: fsm,
		State:    &guardState{Energy: 100},
	})

	// Spawn two scouts driven by a BehaviorTree.
	for i := 0; i < 2; i++ {
		bt := NewBehaviorTree(Selector(
			Sequence(
				Condition(func(self *Actor, w *World) bool {
					return len(w.Neighbors(self.ID, 2.5)) > 0
				}),
				Action(func(dt float64, self *Actor, w *World) NodeStatus {
					neighbors := w.Neighbors(self.ID, 2.5)
					sep := SteerSeparation(self, neighbors, 2.5, 3.5)
					ApplySteering(self, sep, 10.0, 3.5, dt)
					self.Emit(Intent{Kind: "separate", Value: float64(len(neighbors))})
					return StatusSuccess
				}),
			),
			Action(func(dt float64, self *Actor, w *World) NodeStatus {
				rng := w.ActorRNG(self.ID)
				desired := SteerWander(self.Velocity, rng, 0.25, 2.5)
				ApplySteering(self, desired, 8.0, 2.5, dt)
				return StatusRunning
			}),
		))
		world.Spawn(Actor{
			Tag:      "scout",
			Position: Vec2(float64(i*2-1), 0),
			Radius:   1.0,
			Behavior: bt,
			State:    &guardState{Energy: 50},
		})
	}

	return world
}

func assertWorldsIdentical(t *testing.T, step int, w1, w2 *World) {
	t.Helper()
	if math.Float64bits(w1.SimTime()) != math.Float64bits(w2.SimTime()) {
		t.Fatalf("step %d: SimTime mismatch: %v vs %v", step, w1.SimTime(), w2.SimTime())
	}
	if w1.TickCount() != w2.TickCount() {
		t.Fatalf("step %d: TickCount mismatch: %d vs %d", step, w1.TickCount(), w2.TickCount())
	}
	if w1.RNG().Calls() != w2.RNG().Calls() {
		t.Fatalf("step %d: RNG calls mismatch: %d vs %d", step, w1.RNG().Calls(), w2.RNG().Calls())
	}

	a1 := w1.Actors()
	a2 := w2.Actors()
	if len(a1) != len(a2) {
		t.Fatalf("step %d: actor count mismatch: %d vs %d", step, len(a1), len(a2))
	}

	for i := range a1 {
		x, y := a1[i], a2[i]
		if x.ID != y.ID || x.Tag != y.Tag {
			t.Fatalf("step %d actor %d: identity mismatch (%d,%q) vs (%d,%q)", step, i, x.ID, x.Tag, y.ID, y.Tag)
		}
		if !vec3BitsEqual(x.Position, y.Position) {
			t.Fatalf("step %d actor %d: Position mismatch %+v vs %+v", step, x.ID, x.Position, y.Position)
		}
		if !vec3BitsEqual(x.Velocity, y.Velocity) {
			t.Fatalf("step %d actor %d: Velocity mismatch %+v vs %+v", step, x.ID, x.Velocity, y.Velocity)
		}
		s1, ok1 := x.State.(*guardState)
		s2, ok2 := y.State.(*guardState)
		if ok1 != ok2 {
			t.Fatalf("step %d actor %d: state type mismatch", step, x.ID)
		}
		if ok1 {
			if math.Float64bits(s1.Energy) != math.Float64bits(s2.Energy) ||
				s1.Alerts != s2.Alerts ||
				s1.LastTarget != s2.LastTarget {
				t.Fatalf("step %d actor %d: state mismatch %+v vs %+v", step, x.ID, s1, s2)
			}
		}
		if len(x.Intents) != len(y.Intents) {
			t.Fatalf("step %d actor %d: intents count mismatch %d vs %d", step, x.ID, len(x.Intents), len(y.Intents))
		}
		for j := range x.Intents {
			if x.Intents[j] != y.Intents[j] {
				t.Fatalf("step %d actor %d intent %d: mismatch %+v vs %+v", step, x.ID, j, x.Intents[j], y.Intents[j])
			}
		}
	}
}

func vec3BitsEqual(a, b Vec3) bool {
	return math.Float64bits(a.X) == math.Float64bits(b.X) &&
		math.Float64bits(a.Y) == math.Float64bits(b.Y) &&
		math.Float64bits(a.Z) == math.Float64bits(b.Z)
}

func TestHistoricalDeterminism(t *testing.T) {
	const seed uint64 = 987654321

	w1 := buildTestSimulationWorld(seed)
	w2 := buildTestSimulationWorld(seed)

	var frames []Frame
	for i := 0; i < 150; i++ {
		dt := 0.016 + float64(i%7)*0.003
		var evs []Event
		switch i {
		case 10:
			evs = append(evs, Event{
				Kind:   "spawn_intruder",
				Vector: Vec2(-3.5, -4.0),
				Value:  3.2,
			})
		case 45:
			evs = append(evs, Event{
				Kind:   "spawn_intruder",
				Vector: Vec2(2.0, 1.5),
				Value:  2.8,
			})
		case 90:
			evs = append(evs, Event{
				Kind:   "remove_actor",
				Target: 4, // first spawned intruder
			})
		}
		frames = append(frames, Frame{DT: dt, Events: evs})
	}

	d1 := NewHistoricalDriverFromFrames(frames...)
	d2 := NewHistoricalDriverFromFrames(frames...)

	step := 0
	for !d1.Done() {
		dt1, ev1 := d1.Step()
		dt2, ev2 := d2.Step()
		w1.Tick(dt1, ev1)
		w2.Tick(dt2, ev2)
		assertWorldsIdentical(t, step, w1, w2)
		step++
	}

	if step != 150 {
		t.Fatalf("expected 150 steps, got %d", step)
	}
	if w1.RNG().Uint64() != w2.RNG().Uint64() {
		t.Fatalf("final world RNG state diverged")
	}
}

func TestLiveRecordingToHistoricalReplayDeterminism(t *testing.T) {
	const seed uint64 = 42

	liveWorld := buildTestSimulationWorld(seed)

	clockNow := 0.0
	liveDriver := NewLiveDriver(LiveConfig{
		Seed:   seed,
		MaxDT:  0.05,
		Record: true,
		Clock: func() float64 {
			return clockNow
		},
	})

	for i := 0; i < 100; i++ {
		// Variable wall-clock deltas including a spike above MaxDT (0.05).
		delta := 0.012 + float64(i%11)*0.006
		clockNow += delta

		if i == 15 {
			liveDriver.PushEvent(Event{
				Kind:   "spawn_intruder",
				Vector: Vec2(-4.0, -4.0),
				Value:  3.0,
			})
		}
		if i == 60 {
			liveDriver.PushEvent(Event{
				Kind:   "remove_actor",
				Target: 2,
			})
		}

		dt, events := liveDriver.Step()
		liveWorld.Tick(dt, events)
	}

	recordedLog := liveDriver.Log()
	if recordedLog.Seed != seed {
		t.Fatalf("expected recorded seed %d, got %d", seed, recordedLog.Seed)
	}
	if recordedLog.Len() != 100 {
		t.Fatalf("expected 100 recorded frames, got %d", recordedLog.Len())
	}

	replayWorld := buildTestSimulationWorld(recordedLog.Seed)
	histDriver := NewHistoricalDriver(recordedLog)

	for !histDriver.Done() {
		dt, events := histDriver.Step()
		replayWorld.Tick(dt, events)
	}

	assertWorldsIdentical(t, 100, liveWorld, replayWorld)
}

func TestBehaviorsNeverObserveWallClockTime(t *testing.T) {
	// 1. Static AST verification: ensure no library source file imports "time",
	// "math/rand", "math/rand/v2", or "crypto/rand", or calls Now/Since.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	fset := token.NewFileSet()
	checkedFiles := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		checkedFiles++
		fileAST, err := parser.ParseFile(fset, name, nil, parser.AllErrors)
		if err != nil {
			t.Fatalf("failed to parse %s: %v", name, err)
		}
		for _, imp := range fileAST.Imports {
			pkgPath := strings.Trim(imp.Path.Value, `"`)
			switch pkgPath {
			case "time", "math/rand", "math/rand/v2", "crypto/rand":
				t.Fatalf("forbidden import %q found in %s", pkgPath, name)
			}
		}
		ast.Inspect(fileAST, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if ok {
				if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "time" {
					t.Fatalf("forbidden time.%s reference in %s", sel.Sel.Name, name)
				}
			}
			return true
		})
	}
	if checkedFiles == 0 {
		t.Fatal("expected to inspect at least one non-test Go source file")
	}

	// 2. Behavioral verification: two Worlds ticked with the same dt sequence
	// observe identical SimTime and state regardless of real execution work between ticks.
	runWithWork := func(workIterations int) (*World, []float64) {
		w := NewWorld(Config{Seed: 77})
		var observedSimTimes []float64
		w.Spawn(Actor{
			Position: Vec2(1, 2),
			Velocity: Vec2(3, 4),
			Behavior: BehaviorFunc(func(dt float64, self *Actor, world *World) {
				observedSimTimes = append(observedSimTimes, world.SimTime())
				Integrate(self, dt)
			}),
		})
		acc := 0
		for i := 0; i < 25; i++ {
			for j := 0; j < workIterations; j++ {
				acc += j
			}
			w.Tick(0.025, nil)
		}
		_ = acc
		return w, observedSimTimes
	}

	wFast, timesFast := runWithWork(0)
	wSlow, timesSlow := runWithWork(10000)

	if len(timesFast) != len(timesSlow) {
		t.Fatalf("observed tick count mismatch: %d vs %d", len(timesFast), len(timesSlow))
	}
	for i := range timesFast {
		if math.Float64bits(timesFast[i]) != math.Float64bits(timesSlow[i]) {
			t.Fatalf("tick %d: observed SimTime mismatch: %v vs %v", i, timesFast[i], timesSlow[i])
		}
	}
	assertWorldsIdentical(t, 25, wFast, wSlow)
}

func TestDifferentSeedsDiverge(t *testing.T) {
	makeWanderWorld := func(seed uint64) *World {
		w := NewWorld(Config{Seed: seed})
		w.Spawn(Actor{
			Position: Vec2(0, 0),
			Behavior: &WanderBehavior{Speed: 4.0, Jitter: 0.8},
		})
		for i := 0; i < 20; i++ {
			w.Tick(0.05, nil)
		}
		return w
	}

	w1 := makeWanderWorld(100)
	w2 := makeWanderWorld(200)

	p1 := w1.Actors()[0].Position
	p2 := w2.Actors()[0].Position
	if vec3BitsEqual(p1, p2) {
		t.Fatalf("expected different seeds to produce different positions, both got %+v", p1)
	}
}

func TestLifecycleSpawnDestroyDuringTick(t *testing.T) {
	w := NewWorld(Config{Seed: 1})

	var id1, id2, id3 ActorID
	var tickedIDs []ActorID

	id1 = w.Spawn(Actor{
		Tag: "spawner",
		Behavior: BehaviorFunc(func(_ float64, self *Actor, world *World) {
			tickedIDs = append(tickedIDs, self.ID)
			if world.TickCount() == 1 {
				// Destroy actor 2 during tick 1 before actor 2 runs, and spawn actor 4.
				world.Destroy(id2)
				world.Spawn(Actor{
					Tag: "child",
					Behavior: BehaviorFunc(func(_ float64, child *Actor, _ *World) {
						tickedIDs = append(tickedIDs, child.ID)
					}),
				})
			}
		}),
	})
	id2 = w.Spawn(Actor{
		Tag: "victim",
		Behavior: BehaviorFunc(func(_ float64, self *Actor, _ *World) {
			tickedIDs = append(tickedIDs, self.ID)
		}),
	})
	id3 = w.Spawn(Actor{
		Tag:      "observer",
		Behavior: Idle,
	})

	if id1 != 1 || id2 != 2 || id3 != 3 {
		t.Fatalf("unexpected initial IDs: %d, %d, %d", id1, id2, id3)
	}

	w.Tick(0.1, nil)

	// Victim (id2) was destroyed by id1 during tick 1, so it must not have ticked,
	// and child (id4) was spawned during tick 1 so it starts ticking on tick 2.
	if len(tickedIDs) != 1 || tickedIDs[0] != id1 {
		t.Fatalf("expected only actor 1 to tick on frame 1, got %v", tickedIDs)
	}
	if w.ActorByID(id2) != nil {
		t.Fatalf("expected actor 2 to be destroyed")
	}
	if w.Count() != 3 {
		t.Fatalf("expected 3 active actors (1, 3, 4), got %d", w.Count())
	}

	tickedIDs = nil
	w.Tick(0.1, nil)
	if len(tickedIDs) != 2 || tickedIDs[0] != 1 || tickedIDs[1] != 4 {
		t.Fatalf("expected actors [1, 4] to tick on frame 2, got %v", tickedIDs)
	}
}

type customSliceIndex struct {
	actors  []*Actor
	rebuilt int
}

func (c *customSliceIndex) Rebuild(actors []*Actor) {
	c.rebuilt++
	c.actors = append(c.actors[:0], actors...)
}

func (c *customSliceIndex) QueryRadius(center Vec3, radius float64, dst []*Actor) []*Actor {
	rSq := radius * radius
	for _, a := range c.actors {
		if !a.destroyed && a.Position.DistSq(center) <= rSq {
			dst = append(dst, a)
		}
	}
	return dst
}

func TestPerceptionAndSpatialIndex(t *testing.T) {
	idx := &customSliceIndex{}
	w := NewWorld(Config{
		Seed:       5,
		Perception: idx,
	})

	a1 := w.Spawn(Actor{Tag: "unit", Position: Vec2(0, 0)})
	a2 := w.Spawn(Actor{Tag: "unit", Position: Vec2(3, 4)}) // dist 5
	a3 := w.Spawn(Actor{Tag: "base", Position: Vec2(10, 0)})

	w.Tick(0.1, nil)
	if idx.rebuilt == 0 {
		t.Fatal("expected custom SpatialIndex.Rebuild to be called during Tick")
	}

	within5 := w.ActorsWithinRadius(Vec2(0, 0), 5.0)
	if len(within5) != 2 || within5[0].ID != a1 || within5[1].ID != a2 {
		t.Fatalf("unexpected ActorsWithinRadius(5.0): %+v", within5)
	}

	neighbors := w.Neighbors(a1, 6.0)
	if len(neighbors) != 1 || neighbors[0].ID != a2 {
		t.Fatalf("unexpected Neighbors for a1: %+v", neighbors)
	}

	nearest, dist := w.NearestActor(Vec2(0, 0), 20.0, a1)
	if nearest == nil || nearest.ID != a2 || math.Abs(dist-5.0) > 1e-9 {
		t.Fatalf("expected nearest a2 at dist 5, got %+v dist %v", nearest, dist)
	}

	bases := w.FindByTag("base")
	if len(bases) != 1 || bases[0].ID != a3 {
		t.Fatalf("unexpected FindByTag(base): %+v", bases)
	}
}

func TestFSMWildcardAndExplicitSetState(t *testing.T) {
	var log []string
	fsm := NewFSM("idle").
		AddState("idle", FSMState{
			OnEnter: func(*Actor, *World) { log = append(log, "enter:idle") },
			OnExit:  func(*Actor, *World) { log = append(log, "exit:idle") },
		}).
		AddBehaviorState("work", BehaviorFunc(func(_ float64, _ *Actor, _ *World) {
			log = append(log, "tick:work")
		})).
		AddState("emergency", FSMState{
			OnEnter: func(*Actor, *World) { log = append(log, "enter:emergency") },
		}).
		AddTransition("idle", "work", func(_ *Actor, w *World) bool {
			return w.TickCount() == 2
		}).
		AddTransition(WildcardState, "emergency", func(_ *Actor, w *World) bool {
			return len(w.Events()) > 0 && w.Events()[0].Kind == "alarm"
		})

	w := NewWorld(Config{Seed: 1})
	id := w.Spawn(Actor{Behavior: fsm})
	self := w.ActorByID(id)

	w.Tick(0.5, nil) // tick 1: enters idle
	if fsm.Current() != "idle" || fsm.TimeInState() != 0.5 {
		t.Fatalf("expected idle with TimeInState 0.5, got %s (%v)", fsm.Current(), fsm.TimeInState())
	}

	w.Tick(0.5, nil) // tick 2: transitions idle -> work, ticks work
	if fsm.Current() != "work" || fsm.TransitionCount() != 1 {
		t.Fatalf("expected work with 1 transition, got %s (%d)", fsm.Current(), fsm.TransitionCount())
	}

	w.Tick(0.5, []Event{{Kind: "alarm"}}) // tick 3: wildcard transition -> emergency
	if fsm.Current() != "emergency" || fsm.TransitionCount() != 2 {
		t.Fatalf("expected emergency with 2 transitions, got %s (%d)", fsm.Current(), fsm.TransitionCount())
	}

	fsm.SetState("idle", self, w)
	if fsm.Current() != "idle" || fsm.TransitionCount() != 3 {
		t.Fatalf("expected explicit SetState to idle, got %s (%d)", fsm.Current(), fsm.TransitionCount())
	}

	expected := "enter:idle,exit:idle,tick:work,enter:emergency,enter:idle"
	if got := strings.Join(log, ","); got != expected {
		t.Fatalf("unexpected FSM callback log: got %q, want %q", got, expected)
	}
}

func TestBehaviorTreeNodes(t *testing.T) {
	flag := false
	tree := NewBehaviorTree(
		Sequence(
			Invert(Condition(func(*Actor, *World) bool { return flag })),
			Selector(
				Condition(func(*Actor, *World) bool { return false }),
				Action(func(_ float64, self *Actor, _ *World) NodeStatus {
					self.EmitKind("bt_acted", nil)
					return StatusSuccess
				}),
			),
		),
	)

	w := NewWorld(Config{Seed: 1})
	id := w.Spawn(Actor{Behavior: tree})

	w.Tick(0.1, nil)
	if tree.LastStatus() != StatusSuccess {
		t.Fatalf("expected StatusSuccess, got %v", tree.LastStatus())
	}
	if len(w.Intents()) != 1 || w.Intents()[0].ActorID != id || w.Intents()[0].Kind != "bt_acted" {
		t.Fatalf("expected emitted bt_acted intent, got %+v", w.Intents())
	}

	flag = true
	w.Tick(0.1, nil)
	if tree.LastStatus() != StatusFailure {
		t.Fatalf("expected StatusFailure when inverted condition is true, got %v", tree.LastStatus())
	}
}

func TestSteeringAndVectors(t *testing.T) {
	a := Vec3{X: 3, Y: 4, Z: 0}
	if a.Len() != 5.0 {
		t.Fatalf("expected Len 5, got %v", a.Len())
	}
	clamped := a.ClampLen(2.5)
	if math.Abs(clamped.Len()-2.5) > 1e-9 {
		t.Fatalf("expected clamped Len 2.5, got %v", clamped.Len())
	}
	cross := Vec3{X: 1, Y: 0, Z: 0}.Cross(Vec3{X: 0, Y: 1, Z: 0})
	if cross != (Vec3{X: 0, Y: 0, Z: 1}) {
		t.Fatalf("unexpected cross product: %+v", cross)
	}

	flee := SteerFlee(Vec2(5, 0), Vec2(0, 0), 10.0)
	if math.Abs(flee.X-10.0) > 1e-9 || math.Abs(flee.Y) > 1e-9 {
		t.Fatalf("unexpected SteerFlee velocity: %+v", flee)
	}

	arriveSlow := SteerArrive(Vec2(2, 0), Vec2(0, 0), 10.0, 4.0)
	if math.Abs(arriveSlow.Len()-5.0) > 1e-9 {
		t.Fatalf("expected SteerArrive at half slowRadius to have half speed (5.0), got %v", arriveSlow.Len())
	}
}

type counterSource struct {
	state uint64
}

func (c *counterSource) Seed(s uint64) { c.state = s }
func (c *counterSource) Uint64() uint64 {
	c.state += 0x100000001b3
	return c.state
}

func TestPluggableRNG(t *testing.T) {
	rng := NewRNGWithSource(10, &counterSource{})
	if rng.Seed() != 10 {
		t.Fatalf("expected seed 10, got %d", rng.Seed())
	}
	v1 := rng.IntRange(5, 15)
	if v1 < 5 || v1 > 15 {
		t.Fatalf("IntRange out of bounds: %d", v1)
	}
	f := rng.FloatRange(2.0, 4.0)
	if f < 2.0 || f >= 4.0 {
		t.Fatalf("FloatRange out of bounds: %v", f)
	}
	u3 := rng.UnitVec3()
	if math.Abs(u3.Len()-1.0) > 1e-6 {
		t.Fatalf("expected UnitVec3 length 1, got %v", u3.Len())
	}
	rng.Reset(10)
	if rng.Calls() != 0 {
		t.Fatalf("expected Calls() == 0 after Reset, got %d", rng.Calls())
	}
	if rng.IntRange(5, 15) != v1 {
		t.Fatal("expected identical draw after Reset(10)")
	}
}

func TestLiveDriverFixedTimestep(t *testing.T) {
	d := NewLiveDriver(LiveConfig{
		FixedDT: 0.05,
		Record:  true,
	})
	d.Accumulate(0.12)

	dt1, _ := d.Step()
	dt2, _ := d.Step()
	dt3, _ := d.Step()

	if math.Abs(dt1-0.05) > 1e-9 || math.Abs(dt2-0.05) > 1e-9 || dt3 != 0 {
		t.Fatalf("unexpected fixed steps: %v, %v, %v", dt1, dt2, dt3)
	}
	if math.Abs(d.Accumulated()-0.02) > 1e-9 {
		t.Fatalf("expected 0.02 remaining in accumulator, got %v", d.Accumulated())
	}
}

func BenchmarkWorldTick(b *testing.B) {
	w := NewWorld(Config{Seed: 42})
	for i := 0; i < 100; i++ {
		w.Spawn(Actor{
			Position: Vec2(float64(i), float64(i)),
			Velocity: Vec2(1, -1),
			Behavior: BehaviorFunc(func(dt float64, self *Actor, _ *World) {
				Integrate(self, dt)
			}),
		})
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Tick(1.0/60.0, nil)
	}
}

func TestGridIndexEquivalenceAndFlockDeterminism(t *testing.T) {
	spawnFlockWorld := func(seed uint64, index SpatialIndex) *World {
		w := NewWorld(Config{
			Seed:       seed,
			Perception: index,
		})
		w.Spawn(Actor{
			Tag:      "beacon",
			Position: Vec2(8, -5),
			Behavior: Idle,
		})
		w.Spawn(Actor{
			Tag:      "intruder",
			Position: Vec2(-6, 4),
			Behavior: &WanderBehavior{Speed: 5.0, Jitter: 0.4, Center: Vec2(0, 0), MaxRadius: 15.0},
		})
		for i := 0; i < 40; i++ {
			pos := w.RNG().UnitVec2().Scale(w.RNG().FloatRange(1.0, 12.0))
			vel := w.RNG().UnitVec2().Scale(4.0)
			w.Spawn(Actor{
				Tag:      "boid",
				Position: pos,
				Velocity: vel,
				Behavior: &FlockBehavior{
					NeighborRadius:   5.0,
					SeparationRadius: 2.0,
					SeparationWeight: 1.6,
					AlignmentWeight:  1.0,
					CohesionWeight:   1.0,
					WanderJitter:     0.2,
					MaxSpeed:         6.0,
					MaxForce:         18.0,
					SameTagOnly:      true,
					AttractTag:       "beacon",
					AttractWeight:    0.8,
					RepelTag:         "intruder",
					RepelRadius:      6.0,
					RepelWeight:      2.5,
					BoundsCenter:     Vec2(0, 0),
					BoundsRadius:     20.0,
				},
			})
		}
		return w
	}

	wLinear := spawnFlockWorld(777, nil)
	wGrid := spawnFlockWorld(777, NewGridIndex(4.0))

	for step := 0; step < 60; step++ {
		wLinear.Tick(0.05, nil)
		wGrid.Tick(0.05, nil)
		if wLinear.Hash() != wGrid.Hash() {
			t.Fatalf("step %d: GridIndex world hash %x != linear scan world hash %x",
				step, wGrid.Hash(), wLinear.Hash())
		}
		assertWorldsIdentical(t, step, wLinear, wGrid)
	}
}

func TestConePerceptionAndLineOfSight(t *testing.T) {
	// Occlude any target with X > 6.0 (e.g. a wall at X = 6).
	w := NewWorld(Config{
		Seed:       1,
		Perception: NewGridIndex(4.0),
		LineOfSight: func(_, to Vec3) bool {
			return to.X <= 6.0
		},
	})

	idFront := w.Spawn(Actor{Tag: "target", Position: Vec2(4, 0)}) // directly ahead, visible
	_ = w.Spawn(Actor{Tag: "target", Position: Vec2(9, 0)})        // directly ahead, occluded by wall
	_ = w.Spawn(Actor{Tag: "target", Position: Vec2(-3, 0)})       // behind cone
	idAngle := w.Spawn(Actor{Tag: "target", Position: Vec2(3, 2)}) // ~33.7 deg off axis, inside 45 deg half-angle

	visible := w.ActorsWithinCone(Vec2(0, 0), Vec2(1, 0), 12.0, math.Pi/4)
	if len(visible) != 2 || visible[0].ID != idFront || visible[1].ID != idAngle {
		t.Fatalf("expected [idFront=%d, idAngle=%d] in cone, got %+v", idFront, idAngle, visible)
	}
}

func TestEventLogJSONAndBinaryCodecs(t *testing.T) {
	const seed uint64 = 2026
	wOrig := buildTestSimulationWorld(seed)
	live := NewLiveDriver(LiveConfig{
		Seed:      seed,
		DefaultDT: 0.025,
		Record:    true,
	})
	for i := 0; i < 40; i++ {
		if i == 5 {
			live.PushEvent(Event{
				Kind:    "spawn_intruder",
				Vector:  Vec2(-3.25, -4.5),
				Value:   3.125,
				Payload: "wave-1",
			})
		}
		live.Tick(wOrig)
	}
	origHash := wOrig.Hash()
	origLog := live.Log()

	// 1. JSON round-trip
	var jsonBuf bytes.Buffer
	if err := EncodeEventLogJSON(&jsonBuf, origLog); err != nil {
		t.Fatalf("EncodeEventLogJSON failed: %v", err)
	}
	decodedJSON, err := DecodeEventLogJSON(&jsonBuf)
	if err != nil {
		t.Fatalf("DecodeEventLogJSON failed: %v", err)
	}
	wJSON := buildTestSimulationWorld(decodedJSON.Seed)
	NewHistoricalDriver(decodedJSON).RunAll(wJSON)
	if wJSON.Hash() != origHash {
		t.Fatalf("JSON round-trip world hash mismatch: %x vs %x", wJSON.Hash(), origHash)
	}

	// 2. Binary round-trip
	binData, err := MarshalEventLogBinary(origLog)
	if err != nil {
		t.Fatalf("MarshalEventLogBinary failed: %v", err)
	}
	decodedBin, err := UnmarshalEventLogBinary(binData)
	if err != nil {
		t.Fatalf("UnmarshalEventLogBinary failed: %v", err)
	}
	wBin := buildTestSimulationWorld(decodedBin.Seed)
	NewHistoricalDriver(decodedBin).RunAll(wBin)
	if wBin.Hash() != origHash {
		t.Fatalf("Binary round-trip world hash mismatch: %x vs %x", wBin.Hash(), origHash)
	}
}

func TestStatefulBehaviorTreeNodes(t *testing.T) {
	w := NewWorld(Config{Seed: 1})

	// Test MemSequence: child 1 runs once, child 2 returns Running once then Success.
	child1Runs := 0
	child2Runs := 0
	memSeq := MemSequence(
		Action(func(_ float64, _ *Actor, _ *World) NodeStatus {
			child1Runs++
			return StatusSuccess
		}),
		Action(func(_ float64, _ *Actor, _ *World) NodeStatus {
			child2Runs++
			if child2Runs == 1 {
				return StatusRunning
			}
			return StatusSuccess
		}),
	)

	bt := NewBehaviorTree(memSeq)
	w.Spawn(Actor{Behavior: bt})
	w.Tick(0.1, nil)
	if bt.LastStatus() != StatusRunning || child1Runs != 1 || child2Runs != 1 {
		t.Fatalf("tick 1 unexpected: status=%v c1=%d c2=%d", bt.LastStatus(), child1Runs, child2Runs)
	}
	w.Tick(0.1, nil)
	if bt.LastStatus() != StatusSuccess || child1Runs != 1 || child2Runs != 2 {
		t.Fatalf("tick 2 expected MemSequence to resume at child 2 (c1=1, c2=2), got status=%v c1=%d c2=%d",
			bt.LastStatus(), child1Runs, child2Runs)
	}

	// Test Cooldown and Timeout decorators + Parallel composite.
	cooldownNode := Cooldown(0.5, Action(func(_ float64, _ *Actor, _ *World) NodeStatus {
		return StatusSuccess
	}))
	timeoutNode := Timeout(0.35, Action(func(_ float64, _ *Actor, _ *World) NodeStatus {
		return StatusRunning
	}))
	par := Parallel(1, cooldownNode, timeoutNode)

	a := &Actor{}
	// Tick at dt=0.1: cooldown succeeds (starts 0.5s cooldown), timeout is Running -> Parallel(1) succeeds!
	if st := par.Evaluate(0.1, a, w); st != StatusSuccess {
		t.Fatalf("expected Parallel(1) StatusSuccess on first evaluation, got %v", st)
	}
	// Next tick at dt=0.2: cooldown is on cooldown (Failure, 0.3s left), timeout is Running (0.2s elapsed) -> Parallel(1) is Running!
	if st := par.Evaluate(0.2, a, w); st != StatusRunning {
		t.Fatalf("expected Parallel(1) StatusRunning while timeout is running, got %v", st)
	}
	// Next tick at dt=0.2: cooldown still on cooldown (Failure, 0.1s left), timeout hits 0.4s >= 0.3s (Failure) -> Parallel(1) Fails!
	if st := par.Evaluate(0.2, a, w); st != StatusFailure {
		t.Fatalf("expected Parallel(1) StatusFailure when timeout expires during cooldown, got %v", st)
	}
}

func TestAdvancedSteeringHelpers(t *testing.T) {
	self := &Actor{ID: 1, Position: Vec2(0, 0), Velocity: Vec2(5, 0), Radius: 0.5}
	target := &Actor{ID: 2, Position: Vec2(10, 0), Velocity: Vec2(0, 5)}

	pursue := SteerPursue(self, target, 2.0, 10.0)
	if pursue.X <= 0 || pursue.Y <= 0 {
		t.Fatalf("expected SteerPursue to lead moving target into +X,+Y quadrant, got %+v", pursue)
	}

	evade := SteerEvade(self, target, 2.0, 10.0)
	if evade.X >= 0 || evade.Y >= 0 {
		t.Fatalf("expected SteerEvade to flee away into -X,-Y quadrant, got %+v", evade)
	}

	obstacles := []CircleObstacle{{Center: Vec2(4, 0.2), Radius: 1.5}}
	avoid := SteerAvoidCircles(self, obstacles, 6.0, 10.0)
	if avoid.LenSq() == 0 || avoid.Y >= 0 {
		t.Fatalf("expected SteerAvoidCircles to deflect downward (-Y) around obstacle at (4, 0.2), got %+v", avoid)
	}
}

func BenchmarkFlockGridIndex(b *testing.B) {
	w := NewWorld(Config{
		Seed:       42,
		Perception: NewGridIndex(4.0),
	})
	for i := 0; i < 300; i++ {
		w.Spawn(Actor{
			Tag:      "boid",
			Position: w.RNG().UnitVec2().Scale(w.RNG().FloatRange(0, 30)),
			Velocity: w.RNG().UnitVec2().Scale(5.0),
			Behavior: &FlockBehavior{
				NeighborRadius:   4.0,
				SeparationRadius: 1.8,
				MaxSpeed:         6.0,
				MaxForce:         18.0,
				BoundsRadius:     35.0,
			},
		})
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Tick(1.0/60.0, nil)
	}
}

func TestGridMapAStarAndPathSmoothing(t *testing.T) {
	grid := NewGridMap(20, 12, 1.0, Vec2(0, 0))
	// Vertical wall at column 10 from row 0..8, leaving doorway at rows 9..11.
	grid.SetRectWall(10, 0, 10, 8, true)

	start := grid.CellToWorld(2, 2)
	goal := grid.CellToWorld(17, 2)

	if grid.HasLineOfSight(start, goal) {
		t.Fatal("expected wall at x=10 to block direct line of sight")
	}

	rawPath, ok := grid.FindWorldPath(start, goal, false)
	if !ok || len(rawPath) < 4 {
		t.Fatalf("expected valid raw path around wall, got ok=%v len=%d", ok, len(rawPath))
	}
	for _, pt := range rawPath {
		cx, cy, inBounds := grid.WorldToCell(pt)
		if !inBounds || grid.IsWall(cx, cy) {
			t.Fatalf("raw path waypoint %+v lies in wall cell (%d, %d)", pt, cx, cy)
		}
	}

	smoothPath, ok := grid.FindWorldPath(start, goal, true)
	if !ok || len(smoothPath) >= len(rawPath) {
		t.Fatalf("expected smoothed path (%d) to have fewer waypoints than raw path (%d)",
			len(smoothPath), len(rawPath))
	}
	for i := 0; i < len(smoothPath)-1; i++ {
		if !grid.HasLineOfSight(smoothPath[i], smoothPath[i+1]) {
			t.Fatalf("smoothed segment %d -> %d lacks line of sight", i, i+1)
		}
	}

	// Verify FollowPathBehavior steers an Actor all the way to goal without entering a wall.
	w := NewWorld(Config{Seed: 1, LineOfSight: grid.HasLineOfSight})
	follow := &FollowPathBehavior{
		Path:          smoothPath,
		Speed:         6.0,
		MaxForce:      30.0,
		ArrivalRadius: 0.4,
	}
	id := w.Spawn(Actor{
		Position: start,
		Behavior: follow,
	})
	a := w.ActorByID(id)

	for i := 0; i < 200 && !follow.Completed(); i++ {
		w.Tick(0.05, nil)
		cx, cy, ok := grid.WorldToCell(a.Position)
		if !ok || grid.IsWall(cx, cy) {
			t.Fatalf("step %d: actor entered wall cell (%d,%d) at %+v", i, cx, cy, a.Position)
		}
	}
	if !follow.Completed() {
		t.Fatalf("expected actor to complete path within 200 ticks; pos=%+v goal=%+v", a.Position, goal)
	}
}

func TestWeightedGridCostPreference(t *testing.T) {
	grid := NewGridMap(10, 5, 1.0, Vec2(0, 0))
	grid.AllowDiagonal = false
	// Make row 2 across columns 2..7 expensive mud (cost 10.0).
	for x := 2; x <= 7; x++ {
		grid.SetCost(x, 2, 10.0)
	}

	nodes, ok := FindPathNodes(grid, grid.NodeID(1, 2), grid.NodeID(8, 2), 0)
	if !ok {
		t.Fatal("expected valid path")
	}
	for _, nid := range nodes {
		cx, cy := grid.Coords(nid)
		if cy == 2 && cx >= 2 && cx <= 7 {
			t.Fatalf("expected A* to route around high-cost mud cell (%d, %d)", cx, cy)
		}
	}
}

func TestFlowFieldAndCrowdNavigation(t *testing.T) {
	grid := NewGridMap(16, 10, 1.0, Vec2(0, 0))
	grid.SetRectWall(7, 0, 7, 6, true) // Wall with gap at rows 7..9

	flow := NewFlowField(grid)
	goal := grid.CellToWorld(13, 2)
	if !flow.ComputeWorld(goal) {
		t.Fatal("FlowField.ComputeWorld failed")
	}
	if !flow.Valid() {
		t.Fatal("expected FlowField.Valid() == true")
	}

	w := NewWorld(Config{
		Seed:       42,
		Perception: NewGridIndex(2.5),
	})
	for i := 0; i < 8; i++ {
		start := grid.CellToWorld(2, 1+i)
		w.Spawn(Actor{
			Tag:      "crowd",
			Position: start,
			Behavior: &FlowFieldBehavior{
				Field:            flow,
				MaxSpeed:         5.0,
				MaxForce:         25.0,
				SeparationRadius: 0.9,
				SeparationWeight: 0.6,
				ArrivalRadius:    0.8,
			},
		})
	}

	for step := 0; step < 220; step++ {
		w.Tick(0.05, nil)
	}

	arrived := 0
	for _, a := range w.Actors() {
		if a.Position.Dist(goal) <= 2.5 {
			arrived++
		}
	}
	if arrived < 6 {
		t.Fatalf("expected at least 6 of 8 flow-field actors to reach goal neighborhood, got %d", arrived)
	}
}

func TestBlackboardAndSnapshotDeterminism(t *testing.T) {
	w := NewWorld(Config{Seed: 99})
	w.Blackboard().SetString("phase", "alpha")
	w.Blackboard().SetFloat("alarm", 0.5)

	id := w.Spawn(Actor{Tag: "scout", Position: Vec2(2, 3)})
	a := w.ActorByID(id)
	a.Blackboard().SetInt("ammo", 12)
	a.Blackboard().SetBool("alert", true)
	a.Blackboard().SetVec3("home", Vec3{X: 1, Y: 2, Z: 3})
	a.Blackboard().SetActorID("leader", id)

	entries := a.Blackboard().Entries()
	if len(entries) != 4 || entries[0].Key != "alert" || entries[1].Key != "ammo" || entries[2].Key != "home" || entries[3].Key != "leader" {
		t.Fatalf("expected sorted blackboard keys [alert ammo home leader], got %+v", entries)
	}

	h1 := w.Hash()
	a.Blackboard().AddInt("ammo", -1)
	h2 := w.Hash()
	if h1 == h2 {
		t.Fatal("expected World.Hash() to change when Actor Blackboard mutates")
	}

	snapJSON, err := w.SnapshotJSON()
	if err != nil || len(snapJSON) == 0 {
		t.Fatalf("SnapshotJSON failed: %v", err)
	}
}

func TestDeclarativeSpecCompilationHotSwapAndRollout(t *testing.T) {
	reg := NewStandardRegistry(StandardRegistryConfig{})
	catJSON, err := reg.CatalogJSON()
	if err != nil || len(catJSON) == 0 {
		t.Fatalf("CatalogJSON failed: %v", err)
	}

	// Verify strict validation rejects unknown actions and out-of-range params.
	badJSON := []byte(`{"type":"action","name":"teleport_to_moon"}`)
	if _, err := reg.CompileJSON(badJSON); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("expected ErrInvalidSpec for unknown action, got %v", err)
	}
	badParamJSON := []byte(`{"type":"action","name":"wander","params":{"speed":-5}}`)
	if _, err := reg.CompileJSON(badParamJSON); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("expected ErrInvalidSpec for negative speed, got %v", err)
	}

	// Build a Utility + Selector BehaviorSpec in JSON and hot-swap it via EventLog.
	hunterSpecJSON := `{
		"type": "utility",
		"options": [
			{
				"name": "intercept_intruder",
				"weight": 1.2,
				"considerations": [
					{
						"name": "tag_count",
						"params": {"tag": "intruder"},
						"min": 0,
						"max": 1,
						"curve": {"kind": "step", "midpoint": 0.5}
					}
				],
				"child": {
					"type": "action",
					"name": "pursue_tag",
					"params": {
						"tag": "intruder",
						"speed": 10.0,
						"capture_radius": 1.2,
						"destroy_target": true,
						"score_key": "captures",
						"intent": "captured_intruder"
					}
				}
			},
			{
				"name": "collect_beacon",
				"weight": 1.0,
				"considerations": [
					{
						"name": "tag_count",
						"params": {"tag": "beacon"},
						"min": 0,
						"max": 1,
						"curve": {"kind": "step", "midpoint": 0.5}
					}
				],
				"child": {
					"type": "action",
					"name": "arrive_tag",
					"params": {
						"tag": "beacon",
						"speed": 9.0,
						"capture_radius": 1.2,
						"destroy_target": true,
						"score_key": "beacons",
						"intent": "collected_beacon"
					}
				}
			},
			{
				"name": "patrol_perimeter",
				"weight": 0.2,
				"child": {
					"type": "action",
					"name": "patrol_box",
					"params": {"half_width": 6.0, "half_height": 4.0, "speed": 6.0}
				}
			}
		]
	}`

	buildScenario := func(seed uint64) *World {
		w := NewWorld(Config{Seed: seed, Perception: NewGridIndex(3.0)})
		reg.RegisterSwapHandler(w, "set_behavior")
		w.Spawn(Actor{
			Tag:      "unit",
			Position: Vec2(0, 0),
			Behavior: Idle,
		})
		w.Spawn(Actor{
			Tag:      "beacon",
			Position: Vec2(4, 0),
			Behavior: Idle,
		})
		w.Spawn(Actor{
			Tag:      "intruder",
			Position: Vec2(-3, 0),
			Behavior: Idle,
		})
		return w
	}

	// 1. Live run: hot-swap unit #1's behavior at tick 0 via Event, record EventLog,
	// round-trip through binary codec, and verify HistoricalDriver replay matches Hash!
	liveWorld := buildScenario(42)
	live := NewLiveDriver(LiveConfig{Seed: 42, DefaultDT: 0.05, Record: true})
	live.PushEvent(Event{Kind: "set_behavior", Target: 1, Payload: hunterSpecJSON})

	for range 60 {
		dt, evs := live.Step()
		liveWorld.Tick(dt, evs)
	}
	unit := liveWorld.ActorByID(1)
	if unit.Blackboard().FloatOr("captures", 0) != 1 || unit.Blackboard().FloatOr("beacons", 0) != 1 {
		t.Fatalf("expected unit to capture 1 intruder and collect 1 beacon, got captures=%v beacons=%v",
			unit.Blackboard().FloatOr("captures", 0), unit.Blackboard().FloatOr("beacons", 0))
	}

	binBytes, err := MarshalEventLogBinary(live.Log())
	if err != nil {
		t.Fatalf("MarshalEventLogBinary failed: %v", err)
	}
	decodedLog, err := UnmarshalEventLogBinary(binBytes)
	if err != nil {
		t.Fatalf("UnmarshalEventLogBinary failed: %v", err)
	}

	replayWorld := buildScenario(decodedLog.Seed)
	hist := NewHistoricalDriver(decodedLog)
	for !hist.Done() {
		dt, evs := hist.Step()
		replayWorld.Tick(dt, evs)
	}
	if replayWorld.Hash() != liveWorld.Hash() {
		t.Fatalf("historical replay hash mismatch after behavior hot-swap: got 0x%x, want 0x%x",
			replayWorld.Hash(), liveWorld.Hash())
	}

	// 2. Counterfactual rollout comparison: compare hunterSpec against a passive wander spec.
	var hunterSpec BehaviorSpec
	if err := json.Unmarshal([]byte(hunterSpecJSON), &hunterSpec); err != nil {
		t.Fatal(err)
	}
	wanderSpec := BehaviorSpec{
		Type:   "action",
		Name:   "wander",
		Params: map[string]any{"speed": 4.0, "jitter": 0.3},
	}

	ranked, err := CompareRollouts(RolloutConfig{
		BuildWorld:  buildScenario,
		Registry:    reg,
		Seed:        42,
		TargetActor: 1,
		Ticks:       60,
		DT:          0.05,
		Score: func(w *World, _ []IntentSummary) float64 {
			u := w.ActorByID(1)
			return u.Blackboard().FloatOr("captures", 0)*10 + u.Blackboard().FloatOr("beacons", 0)*5
		},
	}, map[string]BehaviorSpec{
		"passive_wander": wanderSpec,
		"utility_hunter": hunterSpec,
	})
	if err != nil {
		t.Fatalf("CompareRollouts failed: %v", err)
	}
	if len(ranked) != 2 || ranked[0].Name != "utility_hunter" || ranked[0].Result.Score <= ranked[1].Result.Score {
		t.Fatalf("expected utility_hunter to rank #1 with higher score, got %+v", ranked)
	}
}

func TestDeclarativeFSMSpecTraceAndOptimize(t *testing.T) {
	reg := NewStandardRegistry(StandardRegistryConfig{})

	// 1. Validate FSM spec error handling.
	badFSM := []byte(`{"type":"fsm","states":[{"id":"idle","child":{"type":"action","name":"wander"}}],"transitions":[{"from":"idle","to":"missing","after_seconds":1.0}]}`)
	if _, err := reg.CompileJSON(badFSM); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("expected ErrInvalidSpec for unknown transition target state, got %v", err)
	}

	// 2. Compile and run a declarative FSM spec with wildcard and timed transitions.
	fsmJSON := `{
		"type": "fsm",
		"initial_state": "patrol",
		"states": [
			{
				"id": "patrol",
				"child": {
					"type": "action",
					"name": "patrol_box",
					"params": {"half_width": 6.0, "half_height": 4.0, "speed": 5.0}
				}
			},
			{
				"id": "intercept",
				"child": {
					"type": "action",
					"name": "pursue_tag",
					"params": {
						"tag": "intruder",
						"speed": 10.0,
						"capture_radius": 1.3,
						"destroy_target": true,
						"score_key": "captures",
						"intent": "captured_intruder"
					}
				}
			},
			{
				"id": "collect",
				"child": {
					"type": "action",
					"name": "arrive_tag",
					"params": {
						"tag": "beacon",
						"speed": 9.0,
						"capture_radius": 1.2,
						"destroy_target": true,
						"score_key": "beacons",
						"intent": "collected_beacon"
					}
				}
			}
		],
		"transitions": [
			{
				"from": "*",
				"to": "intercept",
				"condition": {
					"type": "condition",
					"name": "tag_within_radius",
					"params": {"tag": "intruder", "radius": 25.0}
				}
			},
			{
				"from": "intercept",
				"to": "collect",
				"condition": {
					"type": "invert",
					"child": {
						"type": "condition",
						"name": "tag_within_radius",
						"params": {"tag": "intruder", "radius": 25.0}
					}
				}
			},
			{
				"from": "collect",
				"to": "patrol",
				"after_seconds": 0.2,
				"condition": {
					"type": "invert",
					"child": {
						"type": "condition",
						"name": "tag_within_radius",
						"params": {"tag": "beacon", "radius": 25.0}
					}
				}
			}
		]
	}`

	bt, err := reg.CompileJSON([]byte(fsmJSON))
	if err != nil {
		t.Fatalf("CompileJSON(fsmJSON) failed: %v", err)
	}

	w := NewWorld(Config{Seed: 7, Perception: NewGridIndex(3.0)})
	uid := w.Spawn(Actor{Tag: "unit", Position: Vec2(0, 0), Behavior: bt})
	w.Spawn(Actor{Tag: "intruder", Position: Vec2(3, 0), Behavior: Idle})
	w.Spawn(Actor{Tag: "beacon", Position: Vec2(-2, 0), Behavior: Idle})

	// Tick 1: wildcard transition patrol -> intercept should fire immediately.
	w.Tick(0.05, nil)
	unit := w.ActorByID(uid)
	if got := unit.Blackboard().StringOr("fsm_state", ""); got != "intercept" {
		t.Fatalf("expected fsm_state 'intercept' on tick 1, got %q", got)
	}
	tr, ok := TraceActor(unit)
	if !ok || len(tr.Entries) == 0 || tr.Entries[0].FSMState != "intercept" {
		t.Fatalf("expected ExecutionTrace with FSMState='intercept', got ok=%v trace=%+v", ok, tr)
	}
	if len(tr.ActivePath) < 2 || tr.ActivePath[0] != "fsm:intercept" {
		t.Fatalf("unexpected ActivePath: %+v", tr.ActivePath)
	}

	// Run until intruder and beacon are both cleared and FSM returns to patrol.
	for range 80 {
		w.Tick(0.05, nil)
	}
	if unit.Blackboard().FloatOr("captures", 0) != 1 || unit.Blackboard().FloatOr("beacons", 0) != 1 {
		t.Fatalf("expected FSM unit to capture intruder and collect beacon, got captures=%v beacons=%v",
			unit.Blackboard().FloatOr("captures", 0), unit.Blackboard().FloatOr("beacons", 0))
	}
	if got := unit.Blackboard().StringOr("fsm_state", ""); got != "patrol" {
		t.Fatalf("expected FSM to transition back to 'patrol' after clearing targets, got %q", got)
	}

	// 3. Test MutateSpec and OptimizeSpec determinism and score improvement.
	// Start with a sluggish arrive_tag spec (speed=1.5) that cannot reach all beacons in 50 ticks,
	// and verify OptimizeSpec deterministically tunes parameters to achieve a higher score.
	slowSpec := BehaviorSpec{
		Type: "action",
		Name: "arrive_tag",
		Params: map[string]any{
			"tag":            "beacon",
			"speed":          1.5,
			"max_force":      8.0,
			"capture_radius": 0.8,
			"destroy_target": true,
			"score_key":      "beacons",
		},
	}

	buildOptWorld := func(seed uint64) *World {
		ow := NewWorld(Config{Seed: seed})
		ow.Spawn(Actor{Tag: "unit", Position: Vec2(0, 0), Behavior: Idle})
		ow.Spawn(Actor{Tag: "beacon", Position: Vec2(4, 0), Behavior: Idle})
		ow.Spawn(Actor{Tag: "beacon", Position: Vec2(9, 0), Behavior: Idle})
		return ow
	}

	optCfg := OptimizeConfig{
		Rollout: RolloutConfig{
			BuildWorld:  buildOptWorld,
			Registry:    reg,
			Seed:        101,
			TargetActor: 1,
			Ticks:       50,
			DT:          0.05,
			Score: func(ow *World, _ []IntentSummary) float64 {
				u := ow.ActorByID(1)
				var minDist float64
				for _, b := range ow.FindByTag("beacon") {
					d := u.Position.Dist(b.Position)
					if minDist == 0 || d < minDist {
						minDist = d
					}
				}
				return u.Blackboard().FloatOr("beacons", 0)*20.0 - minDist
			},
		},
		Initial:     slowSpec,
		Generations: 5,
		Population:  6,
		Mutate:      MutateConfig{Registry: reg, Probability: 0.8, Sigma: 0.45},
		Seed:        2026,
	}

	opt1, err := OptimizeSpec(optCfg)
	if err != nil {
		t.Fatalf("OptimizeSpec failed: %v", err)
	}
	opt2, err := OptimizeSpec(optCfg)
	if err != nil {
		t.Fatalf("OptimizeSpec (run 2) failed: %v", err)
	}
	if opt1.BestScore <= opt1.InitialScore {
		t.Fatalf("expected OptimizeSpec to improve score above initial %v, got %v", opt1.InitialScore, opt1.BestScore)
	}
	if opt1.BestResult.WorldHash != opt2.BestResult.WorldHash || math.Float64bits(opt1.BestScore) != math.Float64bits(opt2.BestScore) {
		t.Fatalf("expected OptimizeSpec to be bit-deterministic across runs: %+v vs %+v", opt1.BestResult, opt2.BestResult)
	}
	if len(opt1.BestResult.Traces) == 0 {
		t.Fatal("expected RolloutResult.Traces to contain target Actor ExecutionTrace")
	}
}

func TestReciprocalCollisionAvoidance(t *testing.T) {
	w := NewWorld(Config{Seed: 1, Perception: NewGridIndex(3.0)})

	makeCrossingBehavior := func(goal Vec3) Behavior {
		return BehaviorFunc(func(dt float64, self *Actor, world *World) {
			desired := SteerArrive(self.Position, goal, 5.0, 1.5)
			neighbors := world.Neighbors(self.ID, 6.0)
			avoid := SteerAvoidActors(self, neighbors, 2.0, 5.0)
			if avoid.LenSq() > 0 {
				desired = desired.Add(avoid.Scale(1.4)).ClampLen(5.0)
			}
			ApplySteering(self, desired, 20.0, 5.0, dt)
		})
	}

	id1 := w.Spawn(Actor{
		Tag:      "walker",
		Position: Vec2(-6, 0),
		Velocity: Vec2(5, 0),
		Radius:   0.6,
		Behavior: makeCrossingBehavior(Vec2(6, 0)),
	})
	id2 := w.Spawn(Actor{
		Tag:      "walker",
		Position: Vec2(6, 0),
		Velocity: Vec2(-5, 0),
		Radius:   0.6,
		Behavior: makeCrossingBehavior(Vec2(-6, 0)),
	})

	a1 := w.ActorByID(id1)
	a2 := w.ActorByID(id2)

	minDist := math.MaxFloat64
	for range 80 {
		w.Tick(0.05, nil)
		d := a1.Position.Dist(a2.Position)
		if d < minDist {
			minDist = d
		}
	}

	if minDist < a1.Radius+a2.Radius {
		t.Fatalf("expected head-on actors to avoid overlap (min clearance %.2f), got minDist=%.2f",
			a1.Radius+a2.Radius, minDist)
	}
	if a1.Position.Dist(Vec2(6, 0)) > 1.5 || a2.Position.Dist(Vec2(-6, 0)) > 1.5 {
		t.Fatalf("expected both actors to reach opposite corridor goals, got a1=%+v a2=%+v",
			a1.Position, a2.Position)
	}
}

func TestSquadFormationsAndFollowLeader(t *testing.T) {
	for _, kind := range []FormationKind{FormationWedge, FormationLine, FormationColumn, FormationRing} {
		offs := FormationOffsets(kind, 4, 2.0)
		if len(offs) != 4 {
			t.Fatalf("expected 4 offsets for %s, got %d", kind, len(offs))
		}
	}

	w := NewWorld(Config{Seed: 12, Perception: NewGridIndex(3.0)})
	leaderID := w.Spawn(Actor{
		Tag:      "leader",
		Position: Vec2(0, 0),
		Velocity: Vec2(4, 0),
		Behavior: BehaviorFunc(func(dt float64, self *Actor, _ *World) {
			self.Velocity = Vec2(4, 0)
			Integrate(self, dt)
		}),
	})

	wedge := FormationOffsets(FormationWedge, 4, 2.0)
	var followerIDs []ActorID
	for i, off := range wedge {
		fid := w.Spawn(Actor{
			Tag:      "squad",
			Position: Vec2(-5, float64(i-2)*1.5),
			Behavior: &FollowLeaderBehavior{
				LeaderID:         leaderID,
				SlotOffset:       off,
				MaxSpeed:         7.0,
				MaxForce:         28.0,
				SlowRadius:       2.0,
				SeparationRadius: 1.5,
			},
		})
		followerIDs = append(followerIDs, fid)
	}

	for range 60 {
		w.Tick(0.05, nil)
	}

	leader := w.ActorByID(leaderID)
	for i, fid := range followerIDs {
		f := w.ActorByID(fid)
		expectedSlot := FormationWorldPosition(leader.Position, leader.Velocity, wedge[i])
		if d := f.Position.Dist(expectedSlot); d > 1.8 {
			t.Fatalf("follower %d too far from formation slot %+v: pos=%+v dist=%.2f",
				i, expectedSlot, f.Position, d)
		}
	}
}

func Test3DNavigationWaypointGraphAndVoxelGrid(t *testing.T) {
	// 1. Multi-floor 3D WaypointGraph routing.
	wg := NewWaypointGraph()
	n0 := wg.AddNode(Vec3{X: 0, Y: 0, Z: 0})
	n1 := wg.AddNode(Vec3{X: 5, Y: 0, Z: 0})
	n2 := wg.AddNode(Vec3{X: 5, Y: 0, Z: 8}) // vertical elevator / ramp to Z=8
	n3 := wg.AddNode(Vec3{X: 10, Y: 4, Z: 8})
	wg.AddBiEdge(n0, n1, 1.0)
	wg.AddBiEdge(n1, n2, 1.2)
	wg.AddBiEdge(n2, n3, 1.0)

	wPath, ok := wg.FindWorldPath(Vec3{X: 0.2, Y: 0, Z: 0}, Vec3{X: 10, Y: 4, Z: 8})
	if !ok || len(wPath) < 4 {
		t.Fatalf("expected 3D WaypointGraph path across floors, got ok=%v path=%+v", ok, wPath)
	}

	// 2. Volumetric 3D VoxelGrid routing across a ceiling slab with a vertical shaft opening.
	vg := NewVoxelGrid(10, 8, 5, 1.0, Vec3{X: 0, Y: 0, Z: 0})
	// Block entire Z=2 floor slab except shaft at (8, 6, 2).
	vg.SetBoxWall(0, 0, 2, 9, 7, 2, true)
	vg.SetWall(8, 6, 2, false)

	start := vg.CellToWorld(1, 1, 0)
	goal := vg.CellToWorld(2, 5, 4)

	if vg.HasLineOfSight(start, goal) {
		t.Fatal("expected Z=2 ceiling slab to block 3D line of sight between Z=0 and Z=4")
	}

	raw3D, ok := vg.FindWorldPath(start, goal, false)
	if !ok || len(raw3D) < 5 {
		t.Fatalf("expected valid 3D VoxelGrid path through shaft, got ok=%v len=%d", ok, len(raw3D))
	}
	for _, pt := range raw3D {
		cx, cy, cz, inBounds := vg.WorldToCell(pt)
		if !inBounds || vg.IsWall(cx, cy, cz) {
			t.Fatalf("3D path waypoint %+v lies inside blocked voxel (%d,%d,%d)", pt, cx, cy, cz)
		}
	}

	smooth3D, ok := vg.FindWorldPath(start, goal, true)
	if !ok || len(smooth3D) >= len(raw3D) {
		t.Fatalf("expected smoothed 3D path (%d) to have fewer waypoints than raw 3D path (%d)",
			len(smooth3D), len(raw3D))
	}
}

func TestGOAPMultiStepPlanningReplanningAndSpec(t *testing.T) {
	// 1. Direct PlanGOAP cost-optimal multi-step action synthesis.
	bb := NewBlackboard()
	goal := GOAPGoal{
		Name:     "secure_vault",
		Priority: 1.0,
		Desired:  []GOAPFact{FactBool("in_vault", true)},
	}
	actions := []GOAPAction{
		{
			Name:    "find_explosive",
			Cost:    5.0,
			Effects: []GOAPFact{FactBool("has_explosive", true)},
		},
		{
			Name:          "blast_vault",
			Cost:          5.0,
			Preconditions: []GOAPFact{FactBool("has_explosive", true)},
			Effects:       []GOAPFact{FactBool("in_vault", true)},
		},
		{
			Name:    "find_key",
			Cost:    1.0,
			Effects: []GOAPFact{FactBool("has_key", true)},
		},
		{
			Name:          "unlock_door",
			Cost:          1.0,
			Preconditions: []GOAPFact{FactBool("has_key", true)},
			Effects:       []GOAPFact{FactBool("door_open", true)},
		},
		{
			Name:          "enter_vault",
			Cost:          1.0,
			Preconditions: []GOAPFact{FactBool("door_open", true)},
			Effects:       []GOAPFact{FactBool("in_vault", true)},
		},
	}

	plan, cost, ok := PlanGOAP(nil, nil, bb, goal, actions, 256)
	if !ok || len(plan) != 3 || math.Abs(cost-3.0) > 1e-9 {
		t.Fatalf("expected 3-step optimal plan of cost 3.0, got ok=%v len=%d cost=%v", ok, len(plan), cost)
	}
	if plan[0].Name != "find_key" || plan[1].Name != "unlock_door" || plan[2].Name != "enter_vault" {
		t.Fatalf("unexpected plan order: %s -> %s -> %s", plan[0].Name, plan[1].Name, plan[2].Name)
	}

	// 2. Declarative "goap" BehaviorSpec JSON compilation, sensor-driven execution, and ExecutionTrace.
	reg := NewStandardRegistry(StandardRegistryConfig{})
	goapJSON := `{
		"type": "goap",
		"sensors": [
			{
				"key": "intruder_present",
				"condition": {
					"type": "condition",
					"name": "tag_within_radius",
					"params": {"tag": "intruder", "radius": 30.0}
				}
			},
			{
				"key": "beacon_present",
				"condition": {
					"type": "condition",
					"name": "tag_within_radius",
					"params": {"tag": "beacon", "radius": 30.0}
				}
			}
		],
		"goals": [
			{
				"name": "clear_intruders",
				"priority": 2.0,
				"desired": [{"key": "intruder_present", "value": false}]
			},
			{
				"name": "collect_beacons",
				"priority": 1.0,
				"desired": [{"key": "beacon_present", "value": false}]
			}
		],
		"actions": [
			{
				"name": "acquire_lock",
				"cost": 1.0,
				"preconditions": [
					{"key": "intruder_present", "value": true},
					{"key": "weapons_hot", "value": false}
				],
				"effects": [{"key": "weapons_hot", "value": true}],
				"child": {
					"type": "action",
					"name": "emit_intent",
					"params": {"kind": "weapons_hot"}
				}
			},
			{
				"name": "engage_intruder",
				"cost": 2.0,
				"preconditions": [
					{"key": "intruder_present", "value": true},
					{"key": "weapons_hot", "value": true}
				],
				"effects": [
					{"key": "intruder_present", "value": false},
					{"key": "weapons_hot", "value": false}
				],
				"child": {
					"type": "action",
					"name": "pursue_tag",
					"params": {
						"tag": "intruder",
						"speed": 10.0,
						"capture_radius": 1.3,
						"destroy_target": true,
						"score_key": "captures",
						"intent": "captured_intruder"
					}
				}
			},
			{
				"name": "navigate_beacon",
				"cost": 2.0,
				"preconditions": [{"key": "beacon_present", "value": true}],
				"effects": [{"key": "beacon_present", "value": false}],
				"child": {
					"type": "action",
					"name": "arrive_tag",
					"params": {
						"tag": "beacon",
						"speed": 9.0,
						"capture_radius": 1.2,
						"destroy_target": true,
						"score_key": "beacons",
						"intent": "collected_beacon"
					}
				}
			}
		]
	}`

	bt, err := reg.CompileJSON([]byte(goapJSON))
	if err != nil {
		t.Fatalf("CompileJSON(goapJSON) failed: %v", err)
	}

	w := NewWorld(Config{Seed: 42, Perception: NewGridIndex(3.0)})
	uid := w.Spawn(Actor{Tag: "unit", Position: Vec2(0, 0), Behavior: bt})
	w.Spawn(Actor{Tag: "intruder", Position: Vec2(3, 0), Behavior: Idle})
	w.Spawn(Actor{Tag: "beacon", Position: Vec2(-3, 0), Behavior: Idle})

	// Tick 1: GOAP plans [acquire_lock, engage_intruder] for goal "clear_intruders".
	w.Tick(0.05, nil)
	unit := w.ActorByID(uid)
	if got := unit.Blackboard().StringOr("goap_goal", ""); got != "clear_intruders" {
		t.Fatalf("expected goap_goal 'clear_intruders' on tick 1, got %q", got)
	}
	if got := unit.Blackboard().StringOr("goap_plan", ""); got != "acquire_lock->engage_intruder" {
		t.Fatalf("expected 2-step plan 'acquire_lock->engage_intruder', got %q", got)
	}
	tr, ok := TraceActor(unit)
	if !ok || len(tr.Entries) == 0 || tr.Entries[0].GOAPGoal != "clear_intruders" || len(tr.Entries[0].GOAPPlan) != 2 {
		t.Fatalf("unexpected GOAP ExecutionTrace on tick 1: %+v", tr)
	}

	// Run 60 ticks: unit should execute acquire_lock -> engage_intruder, then replan
	// for collect_beacons -> navigate_beacon!
	for range 60 {
		w.Tick(0.05, nil)
	}
	if unit.Blackboard().FloatOr("captures", 0) != 1 || unit.Blackboard().FloatOr("beacons", 0) != 1 {
		t.Fatalf("expected GOAP unit to capture intruder and collect beacon, got captures=%v beacons=%v",
			unit.Blackboard().FloatOr("captures", 0), unit.Blackboard().FloatOr("beacons", 0))
	}
}

func TestInfluenceMapDecayDiffusionSteeringAndCostRouting(t *testing.T) {
	grid := NewGridMap(11, 11, 1.0, Vec3{})
	im := NewInfluenceMapFromGrid(grid)

	// 1. StampRadiusWithLOS and wall occlusion:
	// Place a wall at (5, 3) between (5, 2) and (5, 4).
	grid.SetWall(5, 3, true)
	im.StampRadiusWithLOS(im.CellToWorld(5, 2), 3.0, 10.0, grid)
	if im.At(5, 2) <= 9.9 {
		t.Fatalf("expected peak influence at (5,2), got %v", im.At(5, 2))
	}
	if im.At(5, 4) != 0 {
		t.Fatalf("expected wall at (5,3) to block LOS stamp at (5,4), got %v", im.At(5, 4))
	}

	// 2. StepWithWalls diffusion + decay:
	// Influence at (5,2) should diffuse to (4,2) and (6,2), but not into wall (5,3) or across it in 1 step.
	beforeCenter := im.At(5, 2)
	im.StepWithWalls(0.2, 0.5, 2.0, grid)
	if im.At(5, 2) >= beforeCenter {
		t.Fatalf("expected decay+diffusion to reduce center peak from %v, got %v", beforeCenter, im.At(5, 2))
	}
	if im.At(5, 3) != 0 {
		t.Fatalf("expected wall cell (5,3) to remain 0 after diffusion, got %v", im.At(5, 3))
	}
	if im.At(4, 2) <= 0 || im.At(6, 2) <= 0 {
		t.Fatalf("expected neighbors (4,2) and (6,2) to have positive influence, got %v, %v", im.At(4, 2), im.At(6, 2))
	}

	// 3. GradientWorld & SteerInfluence (seeking pheromones and fleeing threats):
	im.Clear()
	grid.SetWall(5, 3, false)
	im.StampRadius(im.CellToWorld(5, 5), 4.0, 20.0)

	seeker := &Actor{ID: 1, Position: im.CellToWorld(2, 5)}
	vSeek := SteerInfluence(seeker, im, 6.0, true)
	if vSeek.X <= 0 {
		t.Fatalf("expected pheromone seeker at (2,5) to steer in +X toward peak at (5,5), got %+v", vSeek)
	}
	fleer := &Actor{ID: 2, Position: im.CellToWorld(4, 5)}
	vFlee := SteerInfluence(fleer, im, 6.0, false)
	if vFlee.X >= 0 {
		t.Fatalf("expected threat fleer at (4,5) to steer in -X away from peak at (5,5), got %+v", vFlee)
	}

	// 4. ApplyCostToGridMap coupled with GridMap A* (FindWorldPath):
	start := grid.CellToWorld(1, 5)
	goal := grid.CellToWorld(9, 5)

	// Without influence cost, the path goes straight along y = 5.5 through (5, 5).
	directPath, ok := grid.FindWorldPath(start, goal, false)
	if !ok {
		t.Fatalf("expected direct path to succeed")
	}
	passedCenterBefore := false
	for _, wp := range directPath {
		cx, cy, _ := grid.WorldToCell(wp)
		if cx == 5 && cy == 5 {
			passedCenterBefore = true
		}
	}
	if !passedCenterBefore {
		t.Fatalf("expected unweighted path to pass through center (5,5)")
	}

	// Apply threat influence as traversal cost penalty: A* should detour around (5, 5)!
	im.ApplyCostToGridMap(grid, 1.0, 10.0)
	detourPath, ok := grid.FindWorldPath(start, goal, false)
	if !ok {
		t.Fatalf("expected detour path to succeed")
	}
	for _, wp := range detourPath {
		cx, cy, _ := grid.WorldToCell(wp)
		if cx == 5 && cy == 5 {
			t.Fatalf("expected threat-weighted A* path to detour around high-threat center (5,5), got path %v", detourPath)
		}
	}

	// 5. Declarative BehaviorSpec integration ("stamp_influence", "influence_compare", "steer_influence", "influence_at_self"):
	im.Clear()
	reg := NewStandardRegistry(StandardRegistryConfig{GridMap: grid, InfluenceMap: im})
	specJSON := []byte(`{
		"type": "sequence",
		"children": [
			{"type": "action", "name": "stamp_influence", "params": {"amount": 12.0, "radius": 3.0}},
			{"type": "condition", "name": "influence_compare", "params": {"op": "ge", "value": 10.0}},
			{"type": "action", "name": "steer_influence", "params": {"speed": 6.0, "max_force": 20.0, "seek_high": false}}
		]
	}`)
	bt, err := reg.CompileJSON(specJSON)
	if err != nil {
		t.Fatalf("CompileJSON failed: %v", err)
	}
	w := NewWorld(Config{Seed: 7})
	aid := w.Spawn(Actor{Tag: "scout", Position: im.CellToWorld(5, 5), Behavior: bt})
	w.Tick(0.1, nil)
	scout := w.ActorByID(aid)
	if im.SampleWorld(im.CellToWorld(5, 5)) < 10.0 {
		t.Fatalf("expected stamp_influence to deposit >= 10 at (5,5), got %v", im.SampleWorld(im.CellToWorld(5, 5)))
	}
	if scout.Velocity.LenSq() == 0 {
		t.Fatalf("expected steer_influence (seek_high=false) to accelerate scout away from stamped threat")
	}
}

func TestWorldCheckpointRestoreCloneAndMidSimRollout(t *testing.T) {
	gm := NewGridMap(20, 20, 1.0, Vec2(-10, -10))
	reg := NewStandardRegistry(StandardRegistryConfig{GridMap: gm})

	specJSON := []byte(`{
		"type": "selector",
		"children": [
			{
				"type": "sequence",
				"children": [
					{"type": "condition", "name": "tag_within_radius", "params": {"tag": "beacon", "radius": 20.0}},
					{"type": "action", "name": "follow_path_to_tag", "params": {"tag": "beacon", "speed": 8.0, "capture_radius": 1.2, "destroy_target": true, "score_key": "beacons"}}
				]
			},
			{"type": "action", "name": "wander", "params": {"speed": 5.0, "jitter": 0.4, "max_radius": 8.0}}
		]
	}`)
	bt, err := reg.CompileJSON(specJSON)
	if err != nil {
		t.Fatalf("CompileJSON failed: %v", err)
	}

	w := NewWorld(Config{
		Seed:        99,
		Perception:  NewGridIndex(3.0),
		LineOfSight: gm.HasLineOfSight,
	})
	uid := w.Spawn(Actor{Tag: "unit", Position: Vec2(-5, -2), Behavior: bt})
	w.ActorByID(uid).Blackboard().SetString("behavior_spec", string(specJSON))
	w.Spawn(Actor{Tag: "beacon", Position: Vec2(4, 2), Behavior: Idle})
	w.Spawn(Actor{Tag: "wisp", Position: Vec2(0, 0), Behavior: &WanderBehavior{Speed: 4.0, Jitter: 0.5}})

	// Advance 15 ticks so RNG, ActorRNG, FollowPathBehavior, and Blackboards are mid-flight.
	for range 15 {
		_ = w.RNG().Uint64()
		w.Tick(0.05, nil)
	}

	cp := w.Checkpoint()
	cloned := w.Clone()
	if cloned.Hash() != w.Hash() {
		t.Fatalf("cloned World hash 0x%x != original 0x%x", cloned.Hash(), w.Hash())
	}

	// Step both w and cloned forward 30 ticks (including spawning a new actor at step 10).
	for i := range 30 {
		var evs []Event
		if i == 10 {
			evs = []Event{{Kind: "ping"}}
			w.Spawn(Actor{Tag: "beacon", Position: Vec2(-3, 4), Behavior: Idle})
			cloned.Spawn(Actor{Tag: "beacon", Position: Vec2(-3, 4), Behavior: Idle})
		}
		w.Tick(0.05, evs)
		cloned.Tick(0.05, evs)
	}
	hashAt45 := w.Hash()
	if cloned.Hash() != hashAt45 {
		t.Fatalf("cloned World diverged after 30 ticks: 0x%x vs 0x%x", cloned.Hash(), hashAt45)
	}

	// Now restore w back to tick 15 and step the same 30 ticks again: must match hashAt45 bit-for-bit!
	w.Restore(cp)
	if w.TickCount() != 15 || w.Hash() != cp.WorldHash {
		t.Fatalf("after Restore: tick=%d hash=0x%x, want tick=15 hash=0x%x", w.TickCount(), w.Hash(), cp.WorldHash)
	}
	for i := range 30 {
		var evs []Event
		if i == 10 {
			evs = []Event{{Kind: "ping"}}
			w.Spawn(Actor{Tag: "beacon", Position: Vec2(-3, 4), Behavior: Idle})
		}
		w.Tick(0.05, evs)
	}
	if w.Hash() != hashAt45 {
		t.Fatalf("restored World diverged after re-running 30 ticks: 0x%x vs 0x%x", w.Hash(), hashAt45)
	}

	// Verify JSON round-trip of WorldCheckpoint + RestoreWithRegistry:
	jsonBytes, err := EncodeCheckpointJSON(cp)
	if err != nil {
		t.Fatalf("EncodeCheckpointJSON failed: %v", err)
	}
	decodedCP, err := DecodeCheckpointJSON(jsonBytes)
	if err != nil {
		t.Fatalf("DecodeCheckpointJSON failed: %v", err)
	}
	fresh := NewWorld(Config{Seed: 1, Perception: NewGridIndex(3.0), LineOfSight: gm.HasLineOfSight})
	if err := fresh.RestoreWithRegistry(decodedCP, reg); err != nil {
		t.Fatalf("RestoreWithRegistry failed: %v", err)
	}
	if fresh.Hash() != cp.WorldHash {
		t.Fatalf("RestoreWithRegistry hash 0x%x != checkpoint hash 0x%x", fresh.Hash(), cp.WorldHash)
	}

	// Verify mid-simulation EvaluateRollout using BaseWorld (without BuildWorld):
	w.Restore(cp)
	rolloutRes, err := EvaluateRollout(RolloutConfig{
		BaseWorld: w,
		Registry:  reg,
		TargetTag: "unit",
		Spec: BehaviorSpec{
			Type:   "action",
			Name:   "arrive_tag",
			Params: map[string]any{"tag": "beacon", "speed": 9.0, "capture_radius": 1.2, "destroy_target": true, "score_key": "beacons"},
		},
		Ticks: 40,
		DT:    0.05,
		Score: func(rw *World, _ []IntentSummary) float64 {
			return rw.ActorByID(uid).Blackboard().FloatOr("beacons", 0)
		},
	})
	if err != nil {
		t.Fatalf("mid-sim EvaluateRollout with BaseWorld failed: %v", err)
	}
	if rolloutRes.Score < 1.0 {
		t.Fatalf("expected mid-sim rollout to collect beacon (score >= 1), got %v", rolloutRes.Score)
	}
	// BaseWorld w must remain untouched at tick 15!
	if w.TickCount() != 15 || w.Hash() != cp.WorldHash {
		t.Fatalf("EvaluateRollout mutated BaseWorld: tick=%d hash=0x%x", w.TickCount(), w.Hash())
	}
}

func TestSensoryMemorySoundAndDeclarativeInvestigation(t *testing.T) {
	gm := NewGridMap(20, 20, 1.0, Vec2(0, 0))
	// Wall blocking direct LOS between (2,5) and (15,5) once target moves behind (10, 2..8).
	gm.SetRectWall(10, 2, 10, 8, true)

	reg := NewStandardRegistry(StandardRegistryConfig{GridMap: gm})
	specJSON := []byte(`{
		"type": "selector",
		"children": [
			{
				"type": "sequence",
				"children": [
					{"type": "condition", "name": "perceive_tag", "params": {"tag": "intruder", "radius": 20.0, "decay_rate": 0.15, "min_confidence": 0.1}},
					{"type": "action", "name": "investigate_memory", "params": {"tag": "intruder", "speed": 10.0, "lead_seconds": 0.0, "arrival_radius": 1.2, "capture_radius": 1.2, "destroy_target": true, "score_key": "captures"}}
				]
			},
			{"type": "action", "name": "wander", "params": {"speed": 3.0, "jitter": 0.2, "max_radius": 15.0}}
		]
	}`)
	bt, err := reg.CompileJSON(specJSON)
	if err != nil {
		t.Fatalf("CompileJSON failed: %v", err)
	}

	w := NewWorld(Config{
		Seed:        42,
		Perception:  NewGridIndex(3.0),
		LineOfSight: gm.HasLineOfSight,
	})
	gid := w.Spawn(Actor{Tag: "guard", Position: gm.CellToWorld(2, 5), Behavior: bt})
	iid := w.Spawn(Actor{Tag: "intruder", Position: gm.CellToWorld(6, 5), Behavior: Idle})

	// 1. Tick 1: guard has unobstructed LOS to intruder at (6.5, 5.5) and records it in Memory().
	w.Tick(0.1, nil)
	guard := w.ActorByID(gid)
	rec, ok := guard.Memory().Get(iid)
	if !ok || !rec.Visible || rec.Confidence != 1.0 {
		t.Fatalf("expected guard Memory() to record visible intruder with confidence=1.0, got %+v (ok=%v)", rec, ok)
	}

	// 2. Intruder teleports behind the wall to (15, 5) where LOS from guard is blocked.
	intruder := w.ActorByID(iid)
	intruder.Position = gm.CellToWorld(15, 5)

	// Step 2 ticks: guard no longer has LOS, so Visible becomes false and Confidence decays,
	// but guard still remembers the last known position (6.5, 5.5) and steers toward it!
	for range 2 {
		w.Tick(0.1, nil)
	}
	rec, ok = guard.Memory().Get(iid)
	if !ok || rec.Visible || rec.Confidence >= 1.0 || rec.Confidence < 0.8 {
		t.Fatalf("expected occluded memory with decayed confidence in [0.8, 1.0), got %+v (ok=%v)", rec, ok)
	}
	if guard.Position.X <= 2.6 {
		t.Fatalf("expected guard to advance toward last known position (6.5, 5.5), got pos=%+v", guard.Position)
	}

	// Step until guard reaches the empty last-known position (6.5, 5.5): investigate_memory
	// should clear (Forget) the stale memory record upon arrival!
	for range 20 {
		w.Tick(0.1, nil)
	}
	if _, stillThere := guard.Memory().Get(iid); stillThere {
		t.Fatalf("expected guard to Forget stale memory after arriving at empty last-known position")
	}

	// 3. Test EmitSound acoustic stimulus through walls:
	EmitSound(w, intruder, 15.0, 1.0)
	soundRec, ok := guard.Memory().Get(iid)
	if !ok || soundRec.Sense != SenseSound || soundRec.Visible {
		t.Fatalf("expected EmitSound to record SenseSound memory on guard, got %+v (ok=%v)", soundRec, ok)
	}

	// Checkpoint & Restore preserves SensoryMemory records and World.Hash():
	cp := w.Checkpoint()
	cloned := w.Clone()
	if cloned.Hash() != w.Hash() || cloned.ActorByID(gid).Memory().Len() != 1 {
		t.Fatalf("expected Clone() to preserve SensoryMemory and World.Hash()")
	}
	w.Restore(cp)
	if w.Hash() != cp.WorldHash {
		t.Fatalf("expected Restore(cp) to match cp.WorldHash")
	}
}
