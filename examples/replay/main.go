// Copyright (c) 2026 Neomantra Corp.

// Command replay demonstrates live recording, historical replay determinism,
// and external adapters (terminal glyph grid and abstract 3D scene nodes)
// reading an ntactor.World without the library knowing about rendering.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	actor "nimbleterminal.dev/ntactor"
)

type unitState struct {
	Role    string
	Glyph   rune
	Stamina float64
	Hits    int
}

// SceneNode represents an external 3D engine node transform updated by an adapter.
type SceneNode struct {
	ID      actor.ActorID `json:"id"`
	Tag     string        `json:"tag"`
	Pos     actor.Vec3    `json:"pos"`
	Forward actor.Vec3    `json:"forward"`
	Anim    string        `json:"anim"`
}

// SceneNodeAdapter maps abstract Actor state and Intents onto 3D scene nodes
// (as an nt3d adapter would) without ntactor importing any engine package.
type SceneNodeAdapter struct {
	Nodes map[actor.ActorID]SceneNode
}

func NewSceneNodeAdapter() *SceneNodeAdapter {
	return &SceneNodeAdapter{Nodes: make(map[actor.ActorID]SceneNode)}
}

func (s *SceneNodeAdapter) Sync(w *actor.World) {
	seen := make(map[actor.ActorID]bool, w.Count())
	for _, a := range w.Actors() {
		seen[a.ID] = true
		fwd := a.Velocity.Normalize()
		if fwd.LenSq() == 0 {
			fwd = actor.Vec3{X: 1, Y: 0, Z: 0}
		}
		anim := "idle"
		if a.Velocity.LenSq() > 0.01 {
			anim = "move"
		}
		for _, intent := range a.Intents {
			if intent.Kind == "tag_intruder" || intent.Kind == "alert" {
				anim = intent.Kind
			}
		}
		s.Nodes[a.ID] = SceneNode{
			ID:      a.ID,
			Tag:     a.Tag,
			Pos:     a.Position,
			Forward: fwd,
			Anim:    anim,
		}
	}
	for id := range s.Nodes {
		if !seen[id] {
			delete(s.Nodes, id)
		}
	}
}

// renderGlyphGrid is a pure glyph adapter that projects World actors onto an ASCII grid.
func renderGlyphGrid(w *actor.World, width, height int) string {
	grid := make([][]rune, height)
	for y := range grid {
		grid[y] = make([]rune, width)
		for x := range grid[y] {
			if y == 0 || y == height-1 || x == 0 || x == width-1 {
				grid[y][x] = '·'
			} else {
				grid[y][x] = ' '
			}
		}
	}
	for _, a := range w.Actors() {
		gx := int(math.Round(a.Position.X)) + width/2
		gy := int(math.Round(a.Position.Y)) + height/2
		if gx < 1 || gx >= width-1 || gy < 1 || gy >= height-1 {
			continue
		}
		ch := '?'
		if st, ok := a.State.(*unitState); ok && st.Glyph != 0 {
			ch = st.Glyph
		}
		grid[gy][gx] = ch
	}
	var sb strings.Builder
	for y := range grid {
		sb.WriteString(string(grid[y]))
		if y < len(grid)-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func newSimulationWorld(seed uint64) *actor.World {
	world := actor.NewWorld(actor.Config{Seed: seed})

	world.OnEvent("spawn_intruder", func(w *actor.World, ev actor.Event) {
		w.Spawn(actor.Actor{
			Tag:      "intruder",
			Position: ev.Vector,
			Behavior: &actor.WanderBehavior{
				Speed:     ev.Value,
				Jitter:    0.45,
				Center:    actor.Vec2(0, 0),
				MaxRadius: 10.0,
			},
			State: &unitState{Role: "intruder", Glyph: '!', Stamina: 100},
		})
	})

	// Spawn two guards with Finite State Machines.
	waypoints := [][]actor.Vec3{
		{actor.Vec2(-8, -4), actor.Vec2(8, -4), actor.Vec2(8, 4), actor.Vec2(-8, 4)},
		{actor.Vec2(8, 4), actor.Vec2(-8, 4), actor.Vec2(-8, -4), actor.Vec2(8, -4)},
	}
	for i, wp := range waypoints {
		patrol := &actor.PatrolBehavior{
			Waypoints:     wp,
			Speed:         3.5,
			ArrivalRadius: 0.6,
		}
		fsm := actor.NewFSM("patrol")
		fsm.AddState("patrol", actor.FSMState{
			OnTick: func(dt float64, self *actor.Actor, w *actor.World) {
				patrol.Tick(dt, self, w)
				st := self.State.(*unitState)
				st.Glyph = 'G'
				st.Stamina = math.Min(100, st.Stamina+dt*4.0)
			},
		})
		fsm.AddState("chase", actor.FSMState{
			OnEnter: func(self *actor.Actor, _ *actor.World) {
				self.State.(*unitState).Glyph = 'C'
				self.EmitKind("alert", "intruder_spotted")
			},
			OnTick: func(dt float64, self *actor.Actor, w *actor.World) {
				target, dist := w.NearestActor(self.Position, 9.0, self.ID)
				if target != nil {
					desired := actor.SteerSeek(self.Position, target.Position, 5.0)
					actor.ApplySteering(self, desired, 15.0, 5.0, dt)
					if dist <= 1.2 {
						self.State.(*unitState).Hits++
						self.Emit(actor.Intent{Kind: "tag_intruder", Target: target.ID})
					}
				}
				self.State.(*unitState).Stamina -= dt * 12.0
			},
		})
		fsm.AddTransition("patrol", "chase", func(self *actor.Actor, w *actor.World) bool {
			for _, other := range w.Neighbors(self.ID, 6.5) {
				if other.Tag == "intruder" {
					return true
				}
			}
			return false
		})
		fsm.AddTransition("chase", "patrol", func(self *actor.Actor, w *actor.World) bool {
			if self.State.(*unitState).Stamina <= 15.0 {
				return true
			}
			for _, other := range w.Neighbors(self.ID, 8.0) {
				if other.Tag == "intruder" {
					return false
				}
			}
			return true
		})

		world.Spawn(actor.Actor{
			Tag:      "guard",
			Position: wp[0],
			Behavior: fsm,
			State:    &unitState{Role: fmt.Sprintf("guard-%d", i+1), Glyph: 'G', Stamina: 100},
		})
	}

	// Spawn three scouts controlled by Behavior Trees.
	for i := 0; i < 3; i++ {
		bt := actor.NewBehaviorTree(actor.Selector(
			actor.Sequence(
				actor.Condition(func(self *actor.Actor, w *actor.World) bool {
					return len(w.Neighbors(self.ID, 2.2)) > 0
				}),
				actor.Action(func(dt float64, self *actor.Actor, w *actor.World) actor.NodeStatus {
					sep := actor.SteerSeparation(self, w.Neighbors(self.ID, 2.2), 2.2, 3.0)
					actor.ApplySteering(self, sep, 10.0, 3.0, dt)
					return actor.StatusSuccess
				}),
			),
			actor.Action(func(dt float64, self *actor.Actor, w *actor.World) actor.NodeStatus {
				rng := w.ActorRNG(self.ID)
				desired := actor.SteerWander(self.Velocity, rng, 0.35, 2.8)
				if self.Position.Len() > 8.0 {
					toCenter := actor.SteerSeek(self.Position, actor.Vec2(0, 0), 2.8)
					desired = desired.Lerp(toCenter, 0.4)
				}
				actor.ApplySteering(self, desired, 9.0, 2.8, dt)
				return actor.StatusRunning
			}),
		))
		world.Spawn(actor.Actor{
			Tag:      "scout",
			Position: actor.Vec2(float64(i*3-3), 0),
			Behavior: bt,
			State:    &unitState{Role: fmt.Sprintf("scout-%d", i+1), Glyph: 's', Stamina: 80},
		})
	}

	return world
}

type summaryReport struct {
	Seed               uint64      `json:"seed"`
	Steps              int         `json:"steps"`
	SimTime            float64     `json:"sim_time"`
	Actors             int         `json:"actors"`
	DeterministicMatch bool        `json:"deterministic_match"`
	SceneNodes         []SceneNode `json:"scene_nodes"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("replay", flag.ContinueOnError)
	fs.SetOutput(stderr)
	seed := fs.Uint64("seed", 42, "deterministic world seed")
	steps := fs.Int("steps", 120, "number of simulation ticks to run and replay")
	showASCII := fs.Bool("ascii", true, "render terminal glyph snapshot")
	asJSON := fs.Bool("json", false, "output JSON summary")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	// 1. Run in Live mode with recording enabled.
	liveWorld := newSimulationWorld(*seed)
	liveScene := NewSceneNodeAdapter()
	simClock := 0.0
	liveDriver := actor.NewLiveDriver(actor.LiveConfig{
		Seed:   *seed,
		MaxDT:  0.1,
		Record: true,
		Clock:  func() float64 { return simClock },
	})

	for i := 0; i < *steps; i++ {
		simClock += 0.04 + float64(i%5)*0.005
		if i == 10 {
			liveDriver.PushEvent(actor.Event{
				Kind:   "spawn_intruder",
				Vector: actor.Vec2(-6, -3),
				Value:  3.2,
			})
		}
		if i == 45 {
			liveDriver.PushEvent(actor.Event{
				Kind:   "spawn_intruder",
				Vector: actor.Vec2(5, 2),
				Value:  2.9,
			})
		}
		dt, events := liveDriver.Step()
		liveWorld.Tick(dt, events)
		liveScene.Sync(liveWorld)
	}

	// 2. Replay from seed + recorded EventLog in Historical mode.
	log := liveDriver.Log()
	replayWorld := newSimulationWorld(log.Seed)
	replayScene := NewSceneNodeAdapter()
	histDriver := actor.NewHistoricalDriver(log)

	for !histDriver.Done() {
		dt, events := histDriver.Step()
		replayWorld.Tick(dt, events)
		replayScene.Sync(replayWorld)
	}

	match := worldsMatchBitExact(liveWorld, replayWorld)
	if !match {
		fmt.Fprintln(stderr, "replay: historical replay diverged from live run")
		return 1
	}

	if *asJSON {
		nodes := make([]SceneNode, 0, replayWorld.Count())
		for _, a := range replayWorld.Actors() {
			nodes = append(nodes, replayScene.Nodes[a.ID])
		}
		rep := summaryReport{
			Seed:               *seed,
			Steps:              *steps,
			SimTime:            replayWorld.SimTime(),
			Actors:             replayWorld.Count(),
			DeterministicMatch: match,
			SceneNodes:         nodes,
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
		return 0
	}

	fmt.Fprintf(stdout, "ntactor replay: seed=%d steps=%d simTime=%.3fs actors=%d bitExact=%v\n",
		*seed, *steps, replayWorld.SimTime(), replayWorld.Count(), match)
	if *showASCII {
		fmt.Fprintln(stdout, renderGlyphGrid(replayWorld, 36, 15))
	}
	return 0
}

func worldsMatchBitExact(a, b *actor.World) bool {
	if math.Float64bits(a.SimTime()) != math.Float64bits(b.SimTime()) ||
		a.TickCount() != b.TickCount() ||
		a.Count() != b.Count() {
		return false
	}
	aa, bb := a.Actors(), b.Actors()
	for i := range aa {
		x, y := aa[i], bb[i]
		if x.ID != y.ID || x.Tag != y.Tag {
			return false
		}
		if math.Float64bits(x.Position.X) != math.Float64bits(y.Position.X) ||
			math.Float64bits(x.Position.Y) != math.Float64bits(y.Position.Y) ||
			math.Float64bits(x.Position.Z) != math.Float64bits(y.Position.Z) ||
			math.Float64bits(x.Velocity.X) != math.Float64bits(y.Velocity.X) ||
			math.Float64bits(x.Velocity.Y) != math.Float64bits(y.Velocity.Y) ||
			math.Float64bits(x.Velocity.Z) != math.Float64bits(y.Velocity.Z) {
			return false
		}
		sx := x.State.(*unitState)
		sy := y.State.(*unitState)
		if math.Float64bits(sx.Stamina) != math.Float64bits(sy.Stamina) || sx.Hits != sy.Hits || sx.Glyph != sy.Glyph {
			return false
		}
	}
	return true
}
