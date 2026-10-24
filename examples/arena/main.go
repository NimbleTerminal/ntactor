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
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"nimbleterminal.dev/ntactor"
	"nimbleterminal.dev/ntcanvas"
)

const (
	arenaWidth  = 56
	arenaHeight = 18
	maxLogLines = 5
)

type actorRoleState struct {
	Name         string
	Glyph        string
	BehaviorKind string
	Status       string
	Score        int
}

var guardWaypoints = [][]ntactor.Vec3{
	{ntactor.Vec2(-18, -5), ntactor.Vec2(18, -5), ntactor.Vec2(18, 5), ntactor.Vec2(-18, 5)},
	{ntactor.Vec2(18, 5), ntactor.Vec2(-18, 5), ntactor.Vec2(-18, -5), ntactor.Vec2(18, -5)},
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
		id := world.Spawn(ntactor.Actor{
			Tag:      "beacon",
			Position: pos,
			Behavior: ntactor.Idle,
			State: &actorRoleState{
				Name:         "Beacon",
				Glyph:        "✦",
				BehaviorKind: "Idle",
				Status:       "awaiting scout",
			},
		})
		if b := world.ActorByID(id); b != nil {
			b.Emit(ntactor.Intent{Kind: "beacon_spawned", Vector: pos})
		}
	})

	w.OnEvent("intruder", func(world *ntactor.World, _ ntactor.Event) {
		rng := world.RNG()
		pos := ntactor.Vec2(
			rng.FloatRange(-float64(arenaWidth)/2+4, float64(arenaWidth)/2-4),
			rng.FloatRange(-float64(arenaHeight)/2+3, float64(arenaHeight)/2-3),
		)
		id := world.Spawn(ntactor.Actor{
			Tag:      "intruder",
			Position: pos,
			Behavior: &ntactor.WanderBehavior{
				Speed:     7.0,
				Jitter:    0.5,
				Center:    ntactor.Vec2(0, 0),
				MaxRadius: float64(arenaHeight)/2 - 2,
			},
			State: &actorRoleState{
				Name:         "Intruder",
				Glyph:        "!",
				BehaviorKind: "Wander",
				Status:       "roaming arena",
			},
		})
		if intr := world.ActorByID(id); intr != nil {
			intr.Emit(ntactor.Intent{Kind: "intruder_entered", Vector: pos})
		}
	})

	// Spawn two perimeter guards using an FSM.
	for i, wp := range guardWaypoints {
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
				st.Status = fmt.Sprintf("patrol wp#%d", patrol.CurrentIndex+1)
				self.Emit(ntactor.Intent{Kind: "resume_patrol"})
			},
			OnTick: func(dt float64, self *ntactor.Actor, world *ntactor.World) {
				patrol.Tick(dt, self, world)
				st := self.State.(*actorRoleState)
				st.Status = fmt.Sprintf("patrol wp#%d", patrol.CurrentIndex+1)
			},
		})
		fsm.AddState("intercept", ntactor.FSMState{
			OnEnter: func(self *ntactor.Actor, _ *ntactor.World) {
				st := self.State.(*actorRoleState)
				st.Glyph = "C"
				st.Status = "intercepting"
				self.Emit(ntactor.Intent{Kind: "pursue_intruder"})
			},
			OnTick: func(dt float64, self *ntactor.Actor, world *ntactor.World) {
				var closest *ntactor.Actor
				bestDist := 100.0
				for _, other := range world.FindByTag("intruder") {
					if d := self.Position.Dist(other.Position); d < bestDist {
						bestDist = d
						closest = other
					}
				}
				if closest != nil {
					st := self.State.(*actorRoleState)
					st.Status = fmt.Sprintf("chasing #%d (%.1fm)", closest.ID, bestDist)
					desired := ntactor.SteerSeek(self.Position, closest.Position, 11.0)
					ntactor.ApplySteering(self, desired, 25.0, 11.0, dt)
					if bestDist <= 1.4 {
						st.Score++
						self.Emit(ntactor.Intent{Kind: "captured_intruder", Target: closest.ID})
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
			State: &actorRoleState{
				Name:         fmt.Sprintf("Guard-%d", i+1),
				Glyph:        "G",
				BehaviorKind: "FSM",
				Status:       "patrol wp#1",
			},
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
					st := self.State.(*actorRoleState)
					if best <= 1.2 {
						st.Score++
						st.Glyph = "s"
						st.Status = fmt.Sprintf("collected #%d", target.ID)
						self.Emit(ntactor.Intent{Kind: "collected_beacon", Target: target.ID})
						world.Destroy(target.ID)
						return ntactor.StatusSuccess
					}
					desired := ntactor.SteerArrive(self.Position, target.Position, 9.0, 3.0)
					ntactor.ApplySteering(self, desired, 20.0, 9.0, dt)
					st.Glyph = "S"
					st.Status = fmt.Sprintf("seeking ✦#%d (%.1fm)", target.ID, best)
					return ntactor.StatusRunning
				}),
			),
			ntactor.Action(func(dt float64, self *ntactor.Actor, world *ntactor.World) ntactor.NodeStatus {
				rng := world.ActorRNG(self.ID)
				desired := ntactor.SteerWander(self.Velocity, rng, 0.4, 6.0)
				neighbors := world.Neighbors(self.ID, 3.0)
				sep := ntactor.SteerSeparation(self, neighbors, 3.0, 6.0)
				desired = desired.Add(sep.Scale(0.6)).ClampLen(6.0)
				if math.Abs(self.Position.X) > float64(arenaWidth)/2-4 || math.Abs(self.Position.Y) > float64(arenaHeight)/2-3 {
					toCenter := ntactor.SteerSeek(self.Position, ntactor.Vec2(0, 0), 6.0)
					desired = desired.Lerp(toCenter, 0.5)
				}
				ntactor.ApplySteering(self, desired, 16.0, 6.0, dt)
				st := self.State.(*actorRoleState)
				st.Glyph = "s"
				if len(neighbors) > 0 {
					st.Status = fmt.Sprintf("wandering (sep:%d)", len(neighbors))
				} else {
					st.Status = "wandering"
				}
				return ntactor.StatusRunning
			}),
		))
		w.Spawn(ntactor.Actor{
			Tag:      "scout",
			Position: ntactor.Vec2(float64(i*6-6), 0),
			Behavior: bt,
			State: &actorRoleState{
				Name:         fmt.Sprintf("Scout-%d", i+1),
				Glyph:        "s",
				BehaviorKind: "BTree",
				Status:       "wandering",
			},
		})
	}

	return w
}

// syncCanvasAdapter projects the abstract World state onto an ntcanvas.GraphemeGrid.
func syncCanvasAdapter(grid *ntcanvas.GraphemeGrid, w *ntactor.World, mode string) {
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

	// Draw subtle patrol perimeter waypoints so the user can see the guard patrol loop.
	for _, wp := range guardWaypoints[0] {
		wx := int(math.Round(wp.X)) + arenaWidth/2
		wy := int(math.Round(wp.Y)) + arenaHeight/2
		if wx > 0 && wx < arenaWidth-1 && wy > 0 && wy < arenaHeight-1 {
			grid.SetTextWithStyle(ntcanvas.Point{X: wx, Y: wy}, "·", borderStyle)
		}
	}

	header := fmt.Sprintf(" [%s] tick=%d simTime=%.2fs actors=%d ", mode, w.TickCount(), w.SimTime(), w.Count())
	grid.SetTextWithStyle(ntcanvas.Point{X: 2, Y: 0}, header, ntcanvas.CellStyle{Attributes: ntcanvas.Bold})

	footer := " · : patrol waypoint | origin (0,0) at center "
	if len(footer) < arenaWidth-2 {
		grid.SetTextWithStyle(ntcanvas.Point{X: 2, Y: arenaHeight - 1}, footer, borderStyle)
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
	activity   []string
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
	syncCanvasAdapter(grid, w, "LIVE")
	return arenaModel{
		seed:       seed,
		world:      w,
		liveDriver: live,
		activity: []string{
			fmt.Sprintf("[t=0.00s] World initialized (seed=%d): 2 FSM guards, 3 BehaviorTree scouts", seed),
		},
		grid:   grid,
		canvas: c,
	}
}

func (m *arenaModel) appendActivity(entry string) {
	m.activity = append(m.activity, entry)
	if len(m.activity) > maxLogLines {
		m.activity = m.activity[len(m.activity)-maxLogLines:]
	}
}

func (m *arenaModel) currentModeLabel() string {
	if m.replaying && m.histDriver != nil {
		return fmt.Sprintf("REPLAY %d/%d", m.histDriver.Index(), m.histDriver.Len())
	}
	if m.paused {
		return "PAUSED"
	}
	return "LIVE"
}

func (m *arenaModel) stepOnce() {
	var evs []ntactor.Event
	if m.replaying && m.histDriver != nil {
		if m.histDriver.Done() {
			m.replaying = false
			m.appendActivity(fmt.Sprintf("[t=%.2fs] Historical replay complete — resumed LIVE recording", m.world.SimTime()))
		} else {
			var dt float64
			dt, evs = m.histDriver.Step()
			m.world.Tick(dt, evs)
		}
	} else {
		var dt float64
		dt, evs = m.liveDriver.Step()
		m.world.Tick(dt, evs)
	}

	for _, ev := range evs {
		m.appendActivity(fmt.Sprintf("[t=%.2fs] event injected: %q", m.world.SimTime(), ev.Kind))
	}
	for _, in := range m.world.Intents() {
		switch in.Kind {
		case "beacon_spawned":
			m.appendActivity(fmt.Sprintf("[t=%.2fs] intent: beacon #%d spawned at (%.1f, %.1f) — scouts alerted",
				m.world.SimTime(), in.ActorID, in.Vector.X, in.Vector.Y))
		case "intruder_entered":
			m.appendActivity(fmt.Sprintf("[t=%.2fs] intent: intruder #%d entered at (%.1f, %.1f) — guards intercepting!",
				m.world.SimTime(), in.ActorID, in.Vector.X, in.Vector.Y))
		case "captured_intruder":
			m.appendActivity(fmt.Sprintf("[t=%.2fs] intent: guard #%d captured intruder #%d",
				m.world.SimTime(), in.ActorID, in.Target))
		case "collected_beacon":
			m.appendActivity(fmt.Sprintf("[t=%.2fs] intent: scout #%d collected beacon #%d",
				m.world.SimTime(), in.ActorID, in.Target))
		}
	}

	syncCanvasAdapter(m.grid, m.world, m.currentModeLabel())
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
			syncCanvasAdapter(m.grid, m.world, m.currentModeLabel())
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
				m.activity = []string{
					fmt.Sprintf("[t=0.00s] Replaying %d recorded frames from seed=%d via HistoricalDriver", log.Len(), log.Seed),
				}
				syncCanvasAdapter(m.grid, m.world, m.currentModeLabel())
			}
		case "x":
			m = newArenaModel(m.seed)
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.canvas, cmd = m.canvas.Update(msg)
	return m, cmd
}

func (m arenaModel) renderDashboard() string {
	canvasLines := strings.Split(m.canvas.View(), "\n")

	var rightLines []string
	driverDesc := fmt.Sprintf("LiveDriver (recording frames=%d)", m.liveDriver.Log().Len())
	if m.replaying && m.histDriver != nil {
		driverDesc = fmt.Sprintf("HistoricalDriver (frame %d/%d)", m.histDriver.Index(), m.histDriver.Len())
	}

	rightLines = append(rightLines,
		"SIMULATION & DRIVER",
		fmt.Sprintf("  Driver : %s", driverDesc),
		fmt.Sprintf("  Seed   : %d  |  Clock: %.2fs (dt=%.2fs)", m.world.Seed(), m.world.SimTime(), m.world.DT()),
		"",
		"BEHAVIOR ROLES",
		"  G / C : Guards (FSM: patrol · waypoints <-> intercept !)",
		"  s / S : Scouts (BehaviorTree: seek ✦ beacon | wander+separate)",
		"  ✦     : Beacon (Idle, spawned by external Event)",
		"  !     : Intruder (Wander, spawned by external Event)",
		"",
		"ACTIVE ACTORS (world.Actors() in ID order)",
		"  ID  Glyph  Role       Behavior  State                 Pos          Score",
	)

	actors := m.world.Actors()
	maxShown := 6
	for idx, a := range actors {
		if idx >= maxShown {
			rightLines = append(rightLines, fmt.Sprintf("  ... +%d more actors", len(actors)-maxShown))
			break
		}
		st, _ := a.State.(*actorRoleState)
		if st == nil {
			continue
		}
		rightLines = append(rightLines, fmt.Sprintf(
			"  #%-2d %-5s  %-9s  %-8s  %-20s  (%5.1f,%4.1f)  %d",
			a.ID, st.Glyph, st.Name, st.BehaviorKind, truncate(st.Status, 20),
			a.Position.X, a.Position.Y, st.Score,
		))
	}

	var sb strings.Builder
	sb.WriteString("ntactor Arena — Abstract Actors & Behaviors projected onto an ntcanvas Glyph Grid\n")
	sb.WriteString("Actors own only identity, coordinates, state, and behavior; this UI is an external adapter reading world.Actors() and world.Intents().\n\n")

	totalRows := len(canvasLines)
	if len(rightLines) > totalRows {
		totalRows = len(rightLines)
	}
	for i := 0; i < totalRows; i++ {
		left := ""
		if i < len(canvasLines) {
			left = canvasLines[i]
		} else {
			left = strings.Repeat(" ", arenaWidth)
		}
		right := ""
		if i < len(rightLines) {
			right = rightLines[i]
		}
		sb.WriteString(left)
		if right != "" {
			sb.WriteString("   ")
			sb.WriteString(right)
		}
		sb.WriteByte('\n')
	}

	sb.WriteString("\nRECENT EVENTS & INTENTS (world.Events() / world.Intents()):\n")
	for _, line := range m.activity {
		sb.WriteString("  • ")
		sb.WriteString(line)
		sb.WriteByte('\n')
	}

	sb.WriteString("\nCONTROLS: [b] drop beacon (✦) • [i] spawn intruder (!) • [r] replay recorded history from seed • [space] pause • [n] step tick • [x] reset • [q] quit")
	return sb.String()
}

func truncate(s string, maxLen int) string {
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen-1]) + "…"
}

func (m arenaModel) View() tea.View {
	v := tea.NewView(m.renderDashboard())
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
		fmt.Fprintln(stdout, m.renderDashboard())
		return 0
	}

	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
