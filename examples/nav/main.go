// Copyright (c) 2026 Neomantra Corp.

// Command nav demonstrates deterministic A* pathfinding, line-of-sight path
// smoothing, weighted terrain costs, and Dijkstra FlowField crowd navigation
// over an ntactor.GridMap projected onto an ntcanvas terminal grid.
package main

import (
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"nimbleterminal.dev/ntactor"
	"nimbleterminal.dev/ntactor/examples/internal/demohelp"
	"nimbleterminal.dev/ntcanvas"
)

const (
	mapWidth  = 52
	mapHeight = 16
)

type courierState struct {
	Name   string
	Smooth bool
	Follow *ntactor.FollowPathBehavior
	Trips  int
}

type navSim struct {
	seed       uint64
	gridMap    *ntactor.GridMap
	threatMap  *ntactor.InfluenceMap
	flowField  *ntactor.FlowField
	world      *ntactor.World
	goalCellX  int
	goalCellY  int
	smoothPath bool
}

func buildMazeGrid() *ntactor.GridMap {
	gm := ntactor.NewGridMap(mapWidth, mapHeight, 1.0, ntactor.Vec2(0, 0))
	// Outer border walls.
	gm.SetRectWall(0, 0, mapWidth-1, 0, true)
	gm.SetRectWall(0, mapHeight-1, mapWidth-1, mapHeight-1, true)
	gm.SetRectWall(0, 0, 0, mapHeight-1, true)
	gm.SetRectWall(mapWidth-1, 0, mapWidth-1, mapHeight-1, true)

	// Interior baffle walls forming a multi-room obstacle course.
	gm.SetRectWall(14, 1, 14, 10, true)
	gm.SetRectWall(28, 5, 28, 14, true)
	gm.SetRectWall(40, 1, 40, 9, true)

	// Add a patch of high-cost terrain (mud/sand, cost=3.5) in the middle room
	// so A* and FlowField prefer going around unless the direct path is much shorter.
	for y := 2; y <= 4; y++ {
		for x := 19; x <= 25; x++ {
			gm.SetCost(x, y, 3.5)
		}
	}
	return gm
}

func newNavSim(seed uint64) *navSim {
	gm := buildMazeGrid()
	im := ntactor.NewInfluenceMapFromGrid(gm)
	ff := ntactor.NewFlowField(gm)
	goalX, goalY := 46, 3

	w := ntactor.NewWorld(ntactor.Config{
		Seed:        seed,
		Perception:  ntactor.NewGridIndex(2.5),
		LineOfSight: gm.HasLineOfSight,
	})

	sim := &navSim{
		seed:       seed,
		gridMap:    gm,
		threatMap:  im,
		flowField:  ff,
		world:      w,
		goalCellX:  goalX,
		goalCellY:  goalY,
		smoothPath: true,
	}
	sim.syncCosts()
	ff.Compute(goalX, goalY)

	w.OnEvent("set_goal", func(_ *ntactor.World, ev ntactor.Event) {
		gx, gy := int(ev.Vector.X), int(ev.Vector.Y)
		if gm.InBounds(gx, gy) && !gm.IsWall(gx, gy) {
			sim.goalCellX = gx
			sim.goalCellY = gy
			sim.replanAll()
		}
	})

	w.OnEvent("toggle_wall", func(_ *ntactor.World, ev ntactor.Event) {
		wx, wy := int(ev.Vector.X), int(ev.Vector.Y)
		if wx > 0 && wx < mapWidth-1 && wy > 0 && wy < mapHeight-1 {
			if wx == sim.goalCellX && wy == sim.goalCellY {
				return
			}
			gm.SetWall(wx, wy, !gm.IsWall(wx, wy))
			sim.replanAll()
		}
	})

	w.OnEvent("stamp_threat", func(_ *ntactor.World, ev ntactor.Event) {
		tx, ty := int(ev.Vector.X), int(ev.Vector.Y)
		if gm.InBounds(tx, ty) && !gm.IsWall(tx, ty) {
			im.StampRadiusWithLOS(gm.CellToWorld(tx, ty), 4.0, 10.0, gm)
			sim.replanAll()
		}
	})

	w.OnEvent("toggle_smooth", func(_ *ntactor.World, _ ntactor.Event) {
		sim.smoothPath = !sim.smoothPath
		sim.replanAll()
	})

	w.OnEvent("spawn_crowd", func(world *ntactor.World, _ ntactor.Event) {
		for i := 0; i < 6; i++ {
			start := gm.CellToWorld(3+(i%2), 3+i*2)
			world.Spawn(ntactor.Actor{
				Tag:      "crowd",
				Position: start,
				Behavior: &ntactor.FlowFieldBehavior{
					Field:            ff,
					MaxSpeed:         5.5,
					MaxForce:         25.0,
					SeparationRadius: 1.1,
					SeparationWeight: 0.7,
					ArrivalRadius:    0.9,
				},
			})
		}
	})

	// Spawn two A* Courier Actors that plan paths to the goal, and when they
	// reach the goal, pick a new start corner and plan again.
	starts := [][2]int{{3, 2}, {3, 13}}
	var leaderID ntactor.ActorID
	for i, sc := range starts {
		startPos := gm.CellToWorld(sc[0], sc[1])
		goalPos := gm.CellToWorld(goalX, goalY)
		path, _ := gm.FindWorldPath(startPos, goalPos, sim.smoothPath)
		follow := &ntactor.FollowPathBehavior{
			Path:          path,
			Speed:         7.0,
			MaxForce:      35.0,
			ArrivalRadius: 0.45,
		}
		st := &courierState{
			Name:   fmt.Sprintf("Courier-%d", i+1),
			Smooth: sim.smoothPath,
			Follow: follow,
		}
		id := w.Spawn(ntactor.Actor{
			Tag:      "courier",
			Position: startPos,
			Behavior: ntactor.BehaviorFunc(func(dt float64, self *ntactor.Actor, world *ntactor.World) {
				follow.Tick(dt, self, world)
				if follow.Completed() {
					st.Trips++
					self.Emit(ntactor.Intent{Kind: "goal_reached", Value: float64(st.Trips)})
					// Teleport back to west room and replan to current goal.
					rng := world.ActorRNG(self.ID)
					spawnY := rng.IntRange(2, mapHeight-3)
					self.Position = gm.CellToWorld(3, spawnY)
					self.Velocity = ntactor.Vec3{}
					newGoal := gm.CellToWorld(sim.goalCellX, sim.goalCellY)
					newPath, _ := gm.FindWorldPath(self.Position, newGoal, sim.smoothPath)
					follow.SetPath(newPath)
				}
			}),
			State: st,
		})
		if i == 0 {
			leaderID = id
		}
	}

	// Spawn 2 Wedge formation escort Actors following Courier-1.
	wedgeSlots := ntactor.FormationOffsets(ntactor.FormationWedge, 2, 1.3)
	for i, slot := range wedgeSlots {
		w.Spawn(ntactor.Actor{
			Tag:      "escort",
			Position: gm.CellToWorld(2, 2+i),
			Behavior: &ntactor.FollowLeaderBehavior{
				LeaderID:         leaderID,
				SlotOffset:       slot,
				MaxSpeed:         7.5,
				MaxForce:         32.0,
				SlowRadius:       1.5,
				SeparationRadius: 1.1,
			},
		})
	}

	// Spawn an initial group of 8 FlowField crowd actors.
	for i := 0; i < 8; i++ {
		start := gm.CellToWorld(4+(i%2)*2, 2+i*(mapHeight-4)/8)
		w.Spawn(ntactor.Actor{
			Tag:      "crowd",
			Position: start,
			Behavior: &ntactor.FlowFieldBehavior{
				Field:            ff,
				MaxSpeed:         5.2,
				MaxForce:         24.0,
				SeparationRadius: 1.1,
				SeparationWeight: 0.7,
				ArrivalRadius:    0.9,
			},
		})
	}

	return sim
}

func (s *navSim) syncCosts() {
	s.threatMap.ApplyCostToGridMap(s.gridMap, 1.0, 2.5)
	for y := 2; y <= 4; y++ {
		for x := 19; x <= 25; x++ {
			cost := 3.5 + 2.5*s.threatMap.At(x, y)
			s.gridMap.SetCost(x, y, cost)
		}
	}
}

func (s *navSim) replanAll() {
	s.syncCosts()
	s.flowField.Compute(s.goalCellX, s.goalCellY)
	goalPos := s.gridMap.CellToWorld(s.goalCellX, s.goalCellY)
	for _, a := range s.world.FindByTag("courier") {
		if st, ok := a.State.(*courierState); ok {
			st.Smooth = s.smoothPath
			path, _ := s.gridMap.FindWorldPath(a.Position, goalPos, s.smoothPath)
			st.Follow.SetPath(path)
		}
	}
}

func flowArrow(dir ntactor.Vec3) string {
	if dir.LenSq() < 0.1 {
		return " "
	}
	angle := math.Atan2(dir.Y, dir.X)
	octant := int(math.Round(angle/(math.Pi/4))) & 7
	arrows := [8]string{"→", "↘", "↓", "↙", "←", "↖", "↑", "↗"}
	return arrows[octant]
}

func syncNavCanvas(grid *ntcanvas.GraphemeGrid, sim *navSim, cursorX, cursorY int, showFlow bool, mode string) {
	grid.Clear()
	gm := sim.gridMap
	im := sim.threatMap
	dimStyle := ntcanvas.CellStyle{Attributes: ntcanvas.Dim}
	boldStyle := ntcanvas.CellStyle{Attributes: ntcanvas.Bold}

	for y := 0; y < mapHeight; y++ {
		for x := 0; x < mapWidth; x++ {
			if gm.IsWall(x, y) {
				grid.SetTextWithStyle(ntcanvas.Point{X: x, Y: y}, "█", dimStyle)
				continue
			}
			threat := im.At(x, y)
			if threat >= 2.0 {
				grid.SetTextWithStyle(ntcanvas.Point{X: x, Y: y}, "▓", dimStyle)
				continue
			}
			if threat >= 0.4 {
				grid.SetTextWithStyle(ntcanvas.Point{X: x, Y: y}, "▒", dimStyle)
				continue
			}
			if gm.CellCost(x, y) > 1.5 {
				grid.SetTextWithStyle(ntcanvas.Point{X: x, Y: y}, "░", dimStyle)
				continue
			}
			if showFlow {
				dir := sim.flowField.DirectionAt(gm.CellToWorld(x, y))
				grid.SetTextWithStyle(ntcanvas.Point{X: x, Y: y}, flowArrow(dir), dimStyle)
			}
		}
	}

	// Draw A* Courier planned waypoints.
	for _, a := range sim.world.FindByTag("courier") {
		st, ok := a.State.(*courierState)
		if !ok || st.Follow == nil {
			continue
		}
		for i := st.Follow.CurrentIndex; i < len(st.Follow.Path); i++ {
			wx, wy, ok := gm.WorldToCell(st.Follow.Path[i])
			if ok && !gm.IsWall(wx, wy) {
				grid.SetTextWithStyle(ntcanvas.Point{X: wx, Y: wy}, "·", boldStyle)
			}
		}
	}

	// Draw Goal.
	grid.SetTextWithStyle(ntcanvas.Point{X: sim.goalCellX, Y: sim.goalCellY}, "✦", boldStyle)

	// Draw Crowd, Escort, and Courier Actors.
	for _, a := range sim.world.Actors() {
		cx, cy, ok := gm.WorldToCell(a.Position)
		if !ok {
			continue
		}
		switch a.Tag {
		case "crowd":
			grid.SetText(ntcanvas.Point{X: cx, Y: cy}, "•")
		case "escort":
			grid.SetText(ntcanvas.Point{X: cx, Y: cy}, "v")
		case "courier":
			grid.SetTextWithStyle(ntcanvas.Point{X: cx, Y: cy}, "A", boldStyle)
		}
	}

	// Draw interactive cursor if inside interior.
	if gm.InBounds(cursorX, cursorY) {
		grid.SetTextWithStyle(ntcanvas.Point{X: cursorX, Y: cursorY}, "┼", boldStyle)
	}

	header := fmt.Sprintf(" [%s] tick=%d simTime=%.2fs smooth=%v ",
		mode, sim.world.TickCount(), sim.world.SimTime(), sim.smoothPath)
	grid.SetTextWithStyle(ntcanvas.Point{X: 2, Y: 0}, header, boldStyle)
}

type tickMsg struct{}

func tickCmd() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg {
		return tickMsg{}
	})
}

type navModel struct {
	seed       uint64
	sim        *navSim
	liveDriver *ntactor.LiveDriver
	histDriver *ntactor.HistoricalDriver
	replaying  bool
	paused     bool
	showFlow   bool
	cursorX    int
	cursorY    int
	grid       *ntcanvas.GraphemeGrid
	canvas     ntcanvas.Model
}

func newNavModel(seed uint64) navModel {
	grid, err := ntcanvas.NewGraphemeGridChecked(mapWidth, mapHeight, ntcanvas.SizeLimits{})
	if err != nil {
		panic(err)
	}
	sim := newNavSim(seed)
	live := ntactor.NewLiveDriver(ntactor.LiveConfig{
		Seed:      seed,
		DefaultDT: 0.05,
		Record:    true,
	})
	c := ntcanvas.NewModel(grid, ntcanvas.WithFocus(),
		ntcanvas.WithViewWidth(mapWidth), ntcanvas.WithViewHeight(mapHeight))
	m := navModel{
		seed:       seed,
		sim:        sim,
		liveDriver: live,
		showFlow:   true,
		cursorX:    22,
		cursorY:    8,
		grid:       grid,
		canvas:     c,
	}
	syncNavCanvas(grid, sim, m.cursorX, m.cursorY, m.showFlow, "LIVE")
	return m
}

func (m *navModel) stepOnce() {
	var dt float64
	if m.replaying && m.histDriver != nil {
		if m.histDriver.Done() {
			m.replaying = false
		} else {
			var evs []ntactor.Event
			dt, evs = m.histDriver.Step()
			m.sim.world.Tick(dt, evs)
		}
	} else {
		var evs []ntactor.Event
		dt, evs = m.liveDriver.Step()
		m.sim.world.Tick(dt, evs)
	}
	if dt > 0 {
		m.sim.threatMap.StepWithWalls(dt, 0.08, 0.25, m.sim.gridMap)
		if m.sim.world.TickCount()%5 == 0 {
			m.sim.replanAll()
		}
	}
	mode := "LIVE"
	if m.replaying && m.histDriver != nil {
		mode = fmt.Sprintf("REPLAY %d/%d", m.histDriver.Index(), m.histDriver.Len())
	} else if m.paused {
		mode = "PAUSED"
	}
	syncNavCanvas(m.grid, m.sim, m.cursorX, m.cursorY, m.showFlow, mode)
}

func (m navModel) Init() tea.Cmd {
	return tickCmd()
}

func (m navModel) Help() []key.Binding {
	return []key.Binding{
		demohelp.Key("Toggle wall", "w"),
		demohelp.Key("Stamp threat", "t"),
		demohelp.Key("Move goal", "g"),
		demohelp.Key("Smooth path", "s"),
		demohelp.Key("Spawn crowd", "c"),
		demohelp.Key("Flow arrows", "f"),
		demohelp.Key("Replay log", "r"),
		demohelp.Key("Pause", "space", " "),
		demohelp.Key("Step", "n"),
		demohelp.Key("Quit", "q", "ctrl+c"),
	}
}

func (m navModel) Summary() string {
	mode := "LIVE"
	if m.replaying && m.histDriver != nil {
		mode = fmt.Sprintf("REPLAY %d/%d", m.histDriver.Index(), m.histDriver.Len())
	} else if m.paused {
		mode = "PAUSED"
	}
	return fmt.Sprintf("Nav [%s]: tick=%d goal=(%d,%d) smooth=%v actors=%d hash=0x%016x",
		mode, m.sim.world.TickCount(), m.sim.goalCellX, m.sim.goalCellY, m.sim.smoothPath, m.sim.world.Count(), m.sim.world.Hash())
}

func (m navModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		if !m.paused {
			m.stepOnce()
		}
		return m, tickCmd()
	case tea.KeyMsg:
		if demohelp.Quit(msg) {
			return m, tea.Quit
		}
		switch msg.String() {
		case "space", " ":
			m.paused = !m.paused
		case "n":
			m.stepOnce()
		case "up", "k":
			if m.cursorY > 1 {
				m.cursorY--
			}
		case "down", "j":
			if m.cursorY < mapHeight-2 {
				m.cursorY++
			}
		case "left", "h":
			if m.cursorX > 1 {
				m.cursorX--
			}
		case "right", "l":
			if m.cursorX < mapWidth-2 {
				m.cursorX++
			}
		case "w":
			if !m.replaying {
				m.liveDriver.PushEvent(ntactor.Event{
					Kind:   "toggle_wall",
					Vector: ntactor.Vec2(float64(m.cursorX), float64(m.cursorY)),
				})
				m.stepOnce()
			}
		case "t":
			if !m.replaying {
				m.liveDriver.PushEvent(ntactor.Event{
					Kind:   "stamp_threat",
					Vector: ntactor.Vec2(float64(m.cursorX), float64(m.cursorY)),
				})
				m.stepOnce()
			}
		case "g":
			if !m.replaying {
				m.liveDriver.PushEvent(ntactor.Event{
					Kind:   "set_goal",
					Vector: ntactor.Vec2(float64(m.cursorX), float64(m.cursorY)),
				})
				m.stepOnce()
			}
		case "s":
			if !m.replaying {
				m.liveDriver.PushEvent(ntactor.Event{Kind: "toggle_smooth"})
				m.stepOnce()
			}
		case "c":
			if !m.replaying {
				m.liveDriver.PushEvent(ntactor.Event{Kind: "spawn_crowd"})
				m.stepOnce()
			}
		case "f":
			m.showFlow = !m.showFlow
		case "r":
			log := m.liveDriver.Log()
			if log.Len() > 0 {
				m.sim = newNavSim(log.Seed)
				m.histDriver = ntactor.NewHistoricalDriver(log)
				m.replaying = true
				m.paused = false
			}
		}
		syncNavCanvas(m.grid, m.sim, m.cursorX, m.cursorY, m.showFlow, "LIVE")
	}
	return m, nil
}

func (m navModel) renderView() string {
	var sb strings.Builder
	sb.WriteString("ntactor Navigation — A* Pathfinding, InfluenceMap Threat Routing, Wedge Formations & FlowField Crowd\n")
	fmt.Fprintf(&sb, "Legend: A=A* Courier v=Wedge Escort (·=waypoints) •=FlowField Crowd ✦=Goal █=Wall ▓/▒=Threat ░=Mud ┼=Cursor(%d,%d)\n\n",
		m.cursorX, m.cursorY)
	sb.WriteString(m.canvas.View())
	sb.WriteString("\n\nCONTROLS: [arrows/hjkl] cursor • [w] toggle wall • [t] stamp threat • [g] move goal • [s] toggle smooth • [c] spawn crowd • [f] flow arrows • [r] replay • [q] quit")
	return sb.String()
}

func (m navModel) View() tea.View {
	v := tea.NewView(m.renderView())
	v.AltScreen = true
	return v
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("nav", flag.ContinueOnError)
	fs.SetOutput(stderr)
	seed := fs.Uint64("seed", 42, "deterministic world seed")
	headless := fs.Bool("headless", false, "run headlessly for -steps ticks and print view")
	steps := fs.Int("steps", 50, "number of steps when -headless is set")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	m := newNavModel(*seed)
	if *headless {
		for i := 0; i < *steps; i++ {
			if i == 20 {
				m.liveDriver.PushEvent(ntactor.Event{
					Kind:   "set_goal",
					Vector: ntactor.Vec2(46, 12),
				})
			}
			m.stepOnce()
		}
		fmt.Fprintln(stdout, m.renderView())
		return 0
	}

	if err := demohelp.Run(m); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
