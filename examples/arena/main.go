// Copyright (c) 2026 Neomantra Corp.

// Command arena runs an interactive terminal glyph simulation using ntactor
// and an ntcanvas adapter, supporting live event injection and instant
// historical replay from the recorded seed and event log.
package main

import (
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"nimbleterminal.dev/ntactor"
	"nimbleterminal.dev/ntcanvas"
)

const (
	arenaWidth  = 64
	arenaHeight = 20
)

type actorRoleState struct {
	Glyph  string
	Status string
	Score  int
}

func buildArenaWorld(seed uint64) *ntactor.World {
	w := ntactor.NewWorld(ntactor.Config{Seed: seed})

	w.OnEvent("beacon", func(world *ntactor.World, ev ntactor.Event) {
		pos := ev.Vector
		if pos.LenSq() == 0 {
			rng := world.RNG()
			pos = ntactor.Vec2(
				rng.FloatRange(-float64(arenaWidth)/2+4, float64(arenaWidth)/2-4),
				rng.FloatRange(-float64(arenaHeight)/2+3, float64(arenaHeight)/2-3),
			)
		}
		world.Spawn(ntactor.Actor{
			Tag:      "beacon",
			Position: pos,
			Behavior: ntactor.Idle,
			State:    &actorRoleState{Glyph: "✦", Status: "active"},
		})
	})

	w.OnEvent("intruder", func(world *ntactor.World, _ ntactor.Event) {
		rng := world.RNG()
		pos := ntactor.Vec2(
			rng.FloatRange(-float64(arenaWidth)/2+4, float64(arenaWidth)/2-4),
			rng.FloatRange(-float64(arenaHeight)/2+3, float64(arenaHeight)/2-3),
		)
		world.Spawn(ntactor.Actor{
			Tag:      "intruder",
			Position: pos,
			Behavior: &ntactor.WanderBehavior{
				Speed:     7.0,
				Jitter:    0.5,
				Center:    ntactor.Vec2(0, 0),
				MaxRadius: float64(arenaHeight)/2 - 2,
			},
			State: &actorRoleState{Glyph: "!", Status: "roaming"},
		})
	})

	// Spawn two perimeter guards using an FSM.
	routes := [][]ntactor.Vec3{
		{ntactor.Vec2(-20, -6), ntactor.Vec2(20, -6), ntactor.Vec2(20, 6), ntactor.Vec2(-20, 6)},
		{ntactor.Vec2(20, 6), ntactor.Vec2(-20, 6), ntactor.Vec2(-20, -6), ntactor.Vec2(20, -6)},
	}
	for _, wp := range routes {
		patrol := &ntactor.PatrolBehavior{
			Waypoints:     wp,
			Speed:         8.0,
			ArrivalRadius: 1.0,
		}
		fsm := ntactor.NewFSM("patrol")
		fsm.AddState("patrol", ntactor.FSMState{
			OnEnter: func(self *ntactor.Actor, _ *ntactor.World) {
				st := self.State.(*actorRoleState)
				st.Glyph = "G"
				st.Status = "patrol"
			},
			OnTick: func(dt float64, self *ntactor.Actor, world *ntactor.World) {
				patrol.Tick(dt, self, world)
			},
		})
		fsm.AddState("intercept", ntactor.FSMState{
			OnEnter: func(self *ntactor.Actor, _ *ntactor.World) {
				st := self.State.(*actorRoleState)
				st.Glyph = "C"
				st.Status = "intercept"
			},
			OnTick: func(dt float64, self *ntactor.Actor, world *ntactor.World) {
				var closest *ntactor.Actor
				bestDist := 18.0
				for _, other := range world.FindByTag("intruder") {
					if d := self.Position.Dist(other.Position); d < bestDist {
						bestDist = d
						closest = other
					}
				}
				if closest != nil {
					desired := ntactor.SteerSeek(self.Position, closest.Position, 11.0)
					ntactor.ApplySteering(self, desired, 25.0, 11.0, dt)
					if bestDist <= 1.4 {
						self.State.(*actorRoleState).Score++
						self.Emit(ntactor.Intent{Kind: "captured", Target: closest.ID})
						world.Destroy(closest.ID)
					}
				}
			},
		})
		fsm.AddTransition("patrol", "intercept", func(_ *ntactor.Actor, world *ntactor.World) bool {
			return len(world.FindByTag("intruder")) > 0
		})
		fsm.AddTransition("intercept", "patrol", func(_ *ntactor.Actor, world *ntactor.World) bool {
			return len(world.FindByTag("intruder")) == 0
		})

		w.Spawn(ntactor.Actor{
			Tag:      "guard",
			Position: wp[0],
			Behavior: fsm,
			State:    &actorRoleState{Glyph: "G", Status: "patrol"},
		})
	}

	// Spawn three scouts controlled by Behavior Trees that seek beacons or wander.
	for i := 0; i < 3; i++ {
		bt := ntactor.NewBehaviorTree(ntactor.Selector(
			ntactor.Sequence(
				ntactor.Condition(func(_ *ntactor.Actor, world *ntactor.World) bool {
					return len(world.FindByTag("beacon")) > 0
				}),
				ntactor.Action(func(dt float64, self *ntactor.Actor, world *ntactor.World) ntactor.NodeStatus {
					beacons := world.FindByTag("beacon")
					if len(beacons) == 0 {
						return ntactor.StatusFailure
					}
					target := beacons[0]
					best := self.Position.Dist(target.Position)
					for _, b := range beacons[1:] {
						if d := self.Position.Dist(b.Position); d < best {
							best = d
							target = b
						}
					}
					if best <= 1.2 {
						self.State.(*actorRoleState).Score++
						self.Emit(ntactor.Intent{Kind: "collected", Target: target.ID})
						world.Destroy(target.ID)
						return ntactor.StatusSuccess
					}
					desired := ntactor.SteerArrive(self.Position, target.Position, 9.0, 3.0)
					ntactor.ApplySteering(self, desired, 20.0, 9.0, dt)
					self.State.(*actorRoleState).Status = "seeking"
					return ntactor.StatusRunning
				}),
			),
			ntactor.Action(func(dt float64, self *ntactor.Actor, world *ntactor.World) ntactor.NodeStatus {
				rng := world.ActorRNG(self.ID)
				desired := ntactor.SteerWander(self.Velocity, rng, 0.4, 6.0)
				sep := ntactor.SteerSeparation(self, world.Neighbors(self.ID, 3.0), 3.0, 6.0)
				desired = desired.Add(sep.Scale(0.6)).ClampLen(6.0)
				if math.Abs(self.Position.X) > float64(arenaWidth)/2-4 || math.Abs(self.Position.Y) > float64(arenaHeight)/2-3 {
					toCenter := ntactor.SteerSeek(self.Position, ntactor.Vec2(0, 0), 6.0)
					desired = desired.Lerp(toCenter, 0.5)
				}
				ntactor.ApplySteering(self, desired, 16.0, 6.0, dt)
				self.State.(*actorRoleState).Status = "wandering"
				return ntactor.StatusRunning
			}),
		))
		w.Spawn(ntactor.Actor{
			Tag:      "scout",
			Position: ntactor.Vec2(float64(i*6-6), 0),
			Behavior: bt,
			State:    &actorRoleState{Glyph: "s", Status: "wandering"},
		})
	}

	return w
}

// syncCanvasAdapter projects the abstract World state onto an ntcanvas.GraphemeGrid.
func syncCanvasAdapter(grid *ntcanvas.GraphemeGrid, w *ntactor.World, mode string, lastIntent string) {
	grid.Clear()
	borderStyle := ntcanvas.CellStyle{Attributes: ntcanvas.Dim}
	for x := 0; x < arenaWidth; x++ {
		grid.SetTextWithStyle(ntcanvas.Point{X: x, Y: 0}, "─", borderStyle)
		grid.SetTextWithStyle(ntcanvas.Point{X: x, Y: arenaHeight - 1}, "─", borderStyle)
	}
	for y := 0; y < arenaHeight; y++ {
		grid.SetTextWithStyle(ntcanvas.Point{X: 0, Y: y}, "│", borderStyle)
		grid.SetTextWithStyle(ntcanvas.Point{X: arenaWidth - 1, Y: y}, "│", borderStyle)
	}
	grid.SetTextWithStyle(ntcanvas.Point{X: 0, Y: 0}, "┌", borderStyle)
	grid.SetTextWithStyle(ntcanvas.Point{X: arenaWidth - 1, Y: 0}, "┐", borderStyle)
	grid.SetTextWithStyle(ntcanvas.Point{X: 0, Y: arenaHeight - 1}, "└", borderStyle)
	grid.SetTextWithStyle(ntcanvas.Point{X: arenaWidth - 1, Y: arenaHeight - 1}, "┘", borderStyle)

	header := fmt.Sprintf(" [%s] tick=%d simTime=%.2fs actors=%d ", mode, w.TickCount(), w.SimTime(), w.Count())
	grid.SetTextWithStyle(ntcanvas.Point{X: 2, Y: 0}, header, ntcanvas.CellStyle{Attributes: ntcanvas.Bold})

	if lastIntent != "" {
		grid.SetTextWithStyle(ntcanvas.Point{X: 2, Y: arenaHeight - 1}, " "+lastIntent+" ", borderStyle)
	}

	for _, a := range w.Actors() {
		gx := int(math.Round(a.Position.X)) + arenaWidth/2
		gy := int(math.Round(a.Position.Y)) + arenaHeight/2
		if gx < 1 || gx >= arenaWidth-1 || gy < 1 || gy >= arenaHeight-1 {
			continue
		}
		glyph := "?"
		style := ntcanvas.CellStyle{}
		if st, ok := a.State.(*actorRoleState); ok && st.Glyph != "" {
			glyph = st.Glyph
		}
		if a.Tag == "guard" || a.Tag == "intruder" {
			style.Attributes = ntcanvas.Bold
		}
		grid.SetTextWithStyle(ntcanvas.Point{X: gx, Y: gy}, glyph, style)
	}
}

type tickMsg struct{}

func tickCmd() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg {
		return tickMsg{}
	})
}

type arenaModel struct {
	seed       uint64
	world      *ntactor.World
	liveDriver *ntactor.LiveDriver
	histDriver *ntactor.HistoricalDriver
	replaying  bool
	paused     bool
	lastIntent string
	grid       *ntcanvas.GraphemeGrid
	canvas     ntcanvas.Model
}

func newArenaModel(seed uint64) arenaModel {
	grid, err := ntcanvas.NewGraphemeGridChecked(arenaWidth, arenaHeight, ntcanvas.SizeLimits{})
	if err != nil {
		panic(err)
	}
	w := buildArenaWorld(seed)
	live := ntactor.NewLiveDriver(ntactor.LiveConfig{
		Seed:      seed,
		DefaultDT: 0.05,
		Record:    true,
	})
	c := ntcanvas.NewModel(grid, ntcanvas.WithFocus(),
		ntcanvas.WithViewWidth(arenaWidth), ntcanvas.WithViewHeight(arenaHeight))
	syncCanvasAdapter(grid, w, "LIVE", "")
	return arenaModel{
		seed:       seed,
		world:      w,
		liveDriver: live,
		grid:       grid,
		canvas:     c,
	}
}

func (m *arenaModel) stepOnce() {
	if m.replaying && m.histDriver != nil {
		if m.histDriver.Done() {
			m.replaying = false
		} else {
			dt, evs := m.histDriver.Step()
			m.world.Tick(dt, evs)
		}
	} else {
		dt, evs := m.liveDriver.Step()
		m.world.Tick(dt, evs)
	}
	for _, in := range m.world.Intents() {
		m.lastIntent = fmt.Sprintf("intent: actor#%d -> %s (target#%d)", in.ActorID, in.Kind, in.Target)
	}
	mode := "LIVE"
	if m.replaying {
		mode = fmt.Sprintf("REPLAY %d/%d", m.histDriver.Index(), m.histDriver.Len())
	} else if m.paused {
		mode = "PAUSED"
	}
	syncCanvasAdapter(m.grid, m.world, mode, m.lastIntent)
}

func (m arenaModel) Init() tea.Cmd {
	return tickCmd()
}

func (m arenaModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		if !m.paused {
			m.stepOnce()
		}
		return m, tickCmd()
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "space", " ":
			m.paused = !m.paused
			mode := "LIVE"
			if m.paused {
				mode = "PAUSED"
			}
			syncCanvasAdapter(m.grid, m.world, mode, m.lastIntent)
		case "n":
			m.stepOnce()
		case "b":
			if !m.replaying {
				m.liveDriver.PushEvent(ntactor.Event{Kind: "beacon"})
			}
		case "i":
			if !m.replaying {
				m.liveDriver.PushEvent(ntactor.Event{Kind: "intruder"})
			}
		case "r":
			log := m.liveDriver.Log()
			if log.Len() > 0 {
				m.world = buildArenaWorld(log.Seed)
				m.histDriver = ntactor.NewHistoricalDriver(log)
				m.replaying = true
				m.paused = false
				m.lastIntent = "started deterministic historical replay"
				syncCanvasAdapter(m.grid, m.world, "REPLAY", m.lastIntent)
			}
		}
	}
	var cmd tea.Cmd
	m.canvas, cmd = m.canvas.Update(msg)
	return m, cmd
}

func (m arenaModel) View() tea.View {
	help := "\nb: spawn beacon (✦) • i: spawn intruder (!) • r: replay history • space: pause • n: step • q: quit"
	v := tea.NewView(m.canvas.View() + help)
	v.AltScreen = true
	return v
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("arena", flag.ContinueOnError)
	fs.SetOutput(stderr)
	seed := fs.Uint64("seed", 42, "deterministic world seed")
	headless := fs.Bool("headless", false, "run headlessly for -steps ticks and print canvas")
	steps := fs.Int("steps", 60, "number of steps when -headless is set")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	m := newArenaModel(*seed)
	if *headless {
		for i := 0; i < *steps; i++ {
			if i == 5 {
				m.liveDriver.PushEvent(ntactor.Event{Kind: "beacon"})
			}
			if i == 15 {
				m.liveDriver.PushEvent(ntactor.Event{Kind: "intruder"})
			}
			m.stepOnce()
		}
		fmt.Fprintln(stdout, m.canvas.View())
		return 0
	}

	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
