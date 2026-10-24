// Copyright (c) 2026 Neomantra Corp.

package ntactor

import (
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
