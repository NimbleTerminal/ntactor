// Copyright (c) 2026 Neomantra Corp.

// Command flock demonstrates how ntactor's FlockBehavior and GridIndex drive
// a particle-like murmuration of boid Actors, adapted simultaneously to a
// contiguous 3D point-cloud buffer (as for nt3d.Scene.UpdatePoints) and an
// ntcanvas terminal density grid.
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
	skyWidth  = 60
	skyHeight = 18
)

// PointParticle represents a single point in a contiguous renderer buffer
// (matching the pattern used by nt3d point-cloud updates).
type PointParticle struct {
	X, Y, Z float32
	Speed   float32
}

// PointCloudAdapter packs all "boid" Actors from an ntactor.World into a single
// flat slice of PointParticle values without per-frame heap allocations.
type PointCloudAdapter struct {
	Points []PointParticle
}

func (p *PointCloudAdapter) Sync(w *ntactor.World) {
	p.Points = p.Points[:0]
	for _, a := range w.Actors() {
		if a.Tag != "boid" {
			continue
		}
		p.Points = append(p.Points, PointParticle{
			X:     float32(a.Position.X),
			Y:     float32(a.Position.Y),
			Z:     float32(a.Position.Z),
			Speed: float32(a.Velocity.Len()),
		})
	}
}

func buildFlockWorld(seed uint64, boidCount int) *ntactor.World {
	w := ntactor.NewWorld(ntactor.Config{
		Seed:       seed,
		Perception: ntactor.NewGridIndex(4.0),
	})

	w.OnEvent("attractor", func(world *ntactor.World, _ ntactor.Event) {
		for _, old := range world.FindByTag("attractor") {
			world.Destroy(old.ID)
		}
		rng := world.RNG()
		pos := ntactor.Vec2(
			rng.FloatRange(-float64(skyWidth)/2+6, float64(skyWidth)/2-6),
			rng.FloatRange(-float64(skyHeight)/2+3, float64(skyHeight)/2-3),
		)
		world.Spawn(ntactor.Actor{
			Tag:      "attractor",
			Position: pos,
			Behavior: ntactor.Idle,
		})
	})

	w.OnEvent("predator", func(world *ntactor.World, _ ntactor.Event) {
		rng := world.RNG()
		pos := ntactor.Vec2(
			rng.FloatRange(-float64(skyWidth)/2+5, float64(skyWidth)/2-5),
			rng.FloatRange(-float64(skyHeight)/2+3, float64(skyHeight)/2-3),
		)
		world.Spawn(ntactor.Actor{
			Tag:      "predator",
			Position: pos,
			Velocity: rng.UnitVec2().Scale(9.0),
			Behavior: ntactor.BehaviorFunc(func(dt float64, self *ntactor.Actor, w *ntactor.World) {
				boids := w.Neighbors(self.ID, 14.0)
				desired := ntactor.SteerCohesion(self, boids, 9.5)
				if desired.LenSq() == 0 {
					desired = ntactor.SteerWander(self.Velocity, w.ActorRNG(self.ID), 0.3, 9.5)
				}
				if self.Position.Len() > float64(skyHeight)/2-2 {
					desired = desired.Lerp(ntactor.SteerSeek(self.Position, ntactor.Vec2(0, 0), 9.5), 0.5)
				}
				ntactor.ApplySteering(self, desired, 22.0, 9.5, dt)
			}),
		})
	})

	flockCfg := &ntactor.FlockBehavior{
		NeighborRadius:   4.5,
		SeparationRadius: 1.8,
		SeparationWeight: 1.8,
		AlignmentWeight:  1.1,
		CohesionWeight:   1.0,
		WanderJitter:     0.15,
		MaxSpeed:         8.0,
		MaxForce:         20.0,
		SameTagOnly:      true,
		AttractTag:       "attractor",
		AttractWeight:    1.3,
		RepelTag:         "predator",
		RepelRadius:      7.5,
		RepelWeight:      3.5,
		BoundsCenter:     ntactor.Vec2(0, 0),
		BoundsRadius:     float64(skyHeight)/2 - 2,
	}

	rng := w.RNG()
	for i := 0; i < boidCount; i++ {
		pos := ntactor.Vec3{
			X: rng.FloatRange(-16, 16),
			Y: rng.FloatRange(-5, 5),
			Z: rng.FloatRange(-2, 2),
		}
		vel := rng.UnitVec2().Scale(rng.FloatRange(5.0, 8.0))
		b := *flockCfg
		w.Spawn(ntactor.Actor{
			Tag:      "boid",
			Position: pos,
			Velocity: vel,
			Behavior: &b,
		})
	}

	return w
}

func syncFlockCanvas(grid *ntcanvas.GraphemeGrid, w *ntactor.World, mode string) {
	grid.Clear()
	borderStyle := ntcanvas.CellStyle{Attributes: ntcanvas.Dim}
	for x := 0; x < skyWidth; x++ {
		grid.SetTextWithStyle(ntcanvas.Point{X: x, Y: 0}, "─", borderStyle)
		grid.SetTextWithStyle(ntcanvas.Point{X: x, Y: skyHeight - 1}, "─", borderStyle)
	}
	for y := 0; y < skyHeight; y++ {
		grid.SetTextWithStyle(ntcanvas.Point{X: 0, Y: y}, "│", borderStyle)
		grid.SetTextWithStyle(ntcanvas.Point{X: skyWidth - 1, Y: y}, "│", borderStyle)
	}
	grid.SetTextWithStyle(ntcanvas.Point{X: 0, Y: 0}, "┌", borderStyle)
	grid.SetTextWithStyle(ntcanvas.Point{X: skyWidth - 1, Y: 0}, "┐", borderStyle)
	grid.SetTextWithStyle(ntcanvas.Point{X: 0, Y: skyHeight - 1}, "└", borderStyle)
	grid.SetTextWithStyle(ntcanvas.Point{X: skyWidth - 1, Y: skyHeight - 1}, "┘", borderStyle)

	header := fmt.Sprintf(" [%s] tick=%d simTime=%.2fs actors=%d ", mode, w.TickCount(), w.SimTime(), w.Count())
	grid.SetTextWithStyle(ntcanvas.Point{X: 2, Y: 0}, header, ntcanvas.CellStyle{Attributes: ntcanvas.Bold})

	footer := " ∙:1 boid  •:2  ◦:3  ✶:4+ cluster  ✦:Attractor  ▲:Predator "
	if len([]rune(footer)) < skyWidth-2 {
		grid.SetTextWithStyle(ntcanvas.Point{X: 2, Y: skyHeight - 1}, footer, borderStyle)
	}

	var density [skyHeight][skyWidth]int
	for _, a := range w.Actors() {
		gx := int(math.Round(a.Position.X)) + skyWidth/2
		gy := int(math.Round(a.Position.Y)) + skyHeight/2
		if gx < 1 || gx >= skyWidth-1 || gy < 1 || gy >= skyHeight-1 {
			continue
		}
		if a.Tag == "boid" {
			density[gy][gx]++
		}
	}

	for y := 1; y < skyHeight-1; y++ {
		for x := 1; x < skyWidth-1; x++ {
			n := density[y][x]
			if n == 0 {
				continue
			}
			glyph := "∙"
			style := ntcanvas.CellStyle{}
			switch {
			case n == 2:
				glyph = "•"
			case n == 3:
				glyph = "◦"
			case n >= 4:
				glyph = "✶"
				style.Attributes = ntcanvas.Bold
			}
			grid.SetTextWithStyle(ntcanvas.Point{X: x, Y: y}, glyph, style)
		}
	}

	for _, a := range w.Actors() {
		if a.Tag == "boid" {
			continue
		}
		gx := int(math.Round(a.Position.X)) + skyWidth/2
		gy := int(math.Round(a.Position.Y)) + skyHeight/2
		if gx < 1 || gx >= skyWidth-1 || gy < 1 || gy >= skyHeight-1 {
			continue
		}
		switch a.Tag {
		case "attractor":
			grid.SetTextWithStyle(ntcanvas.Point{X: gx, Y: gy}, "✦", ntcanvas.CellStyle{Attributes: ntcanvas.Bold})
		case "predator":
			grid.SetTextWithStyle(ntcanvas.Point{X: gx, Y: gy}, "▲", ntcanvas.CellStyle{Attributes: ntcanvas.Bold})
		}
	}
}

type tickMsg struct{}

func tickCmd() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg {
		return tickMsg{}
	})
}

type flockModel struct {
	seed       uint64
	boidCount  int
	world      *ntactor.World
	liveDriver *ntactor.LiveDriver
	histDriver *ntactor.HistoricalDriver
	cloud      PointCloudAdapter
	replaying  bool
	paused     bool
	grid       *ntcanvas.GraphemeGrid
	canvas     ntcanvas.Model
}

func newFlockModel(seed uint64, boidCount int) flockModel {
	grid, err := ntcanvas.NewGraphemeGridChecked(skyWidth, skyHeight, ntcanvas.SizeLimits{})
	if err != nil {
		panic(err)
	}
	w := buildFlockWorld(seed, boidCount)
	live := ntactor.NewLiveDriver(ntactor.LiveConfig{
		Seed:      seed,
		DefaultDT: 0.05,
		Record:    true,
	})
	var cloud PointCloudAdapter
	cloud.Sync(w)
	c := ntcanvas.NewModel(grid, ntcanvas.WithFocus(),
		ntcanvas.WithViewWidth(skyWidth), ntcanvas.WithViewHeight(skyHeight))
	syncFlockCanvas(grid, w, "LIVE")
	return flockModel{
		seed:       seed,
		boidCount:  boidCount,
		world:      w,
		liveDriver: live,
		cloud:      cloud,
		grid:       grid,
		canvas:     c,
	}
}

func (m *flockModel) stepOnce() {
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
	m.cloud.Sync(m.world)
	mode := "LIVE"
	if m.replaying && m.histDriver != nil {
		mode = fmt.Sprintf("REPLAY %d/%d", m.histDriver.Index(), m.histDriver.Len())
	} else if m.paused {
		mode = "PAUSED"
	}
	syncFlockCanvas(m.grid, m.world, mode)
}

func (m flockModel) Init() tea.Cmd {
	return tickCmd()
}

func (m flockModel) Help() []key.Binding {
	return []key.Binding{
		demohelp.Key("Attractor", "a"),
		demohelp.Key("Predator", "p"),
		demohelp.Key("Replay log", "r"),
		demohelp.Key("Pause", "space", " "),
		demohelp.Key("Step", "n"),
		demohelp.Key("Reset", "x"),
		demohelp.Key("Quit", "q", "ctrl+c"),
	}
}

func (m flockModel) Summary() string {
	mode := "LIVE"
	if m.replaying && m.histDriver != nil {
		mode = fmt.Sprintf("REPLAY %d/%d", m.histDriver.Index(), m.histDriver.Len())
	} else if m.paused {
		mode = "PAUSED"
	}
	return fmt.Sprintf("Flock [%s]: tick=%d boids=%d predators=%d attractors=%d hash=0x%016x",
		mode, m.world.TickCount(), len(m.cloud.Points), len(m.world.FindByTag("predator")), len(m.world.FindByTag("attractor")), m.world.Hash())
}

func (m flockModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		case "a":
			if !m.replaying {
				m.liveDriver.PushEvent(ntactor.Event{Kind: "attractor"})
				m.stepOnce()
			}
		case "p":
			if !m.replaying {
				m.liveDriver.PushEvent(ntactor.Event{Kind: "predator"})
				m.stepOnce()
			}
		case "r":
			log := m.liveDriver.Log()
			if log.Len() > 0 {
				m.world = buildFlockWorld(log.Seed, m.boidCount)
				m.histDriver = ntactor.NewHistoricalDriver(log)
				m.replaying = true
				m.paused = false
			}
		case "x":
			m = newFlockModel(m.seed, m.boidCount)
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.canvas, cmd = m.canvas.Update(msg)
	return m, cmd
}

func (m flockModel) renderView() string {
	var avgSpeed float64
	for _, pt := range m.cloud.Points {
		avgSpeed += float64(pt.Speed)
	}
	if len(m.cloud.Points) > 0 {
		avgSpeed /= float64(len(m.cloud.Points))
	}

	var sb strings.Builder
	sb.WriteString("ntactor Flock / Murmuration — Boid Actors (Separation + Alignment + Cohesion) with GridIndex\n")
	fmt.Fprintf(&sb, "PointCloudAdapter: %d points packed | avgSpeed=%.2f | predators=%d | attractors=%d | hash=0x%016x\n\n",
		len(m.cloud.Points), avgSpeed, len(m.world.FindByTag("predator")), len(m.world.FindByTag("attractor")), m.world.Hash())
	sb.WriteString(m.canvas.View())
	sb.WriteString("\n\nCONTROLS: [a] move attractor (✦) • [p] spawn predator (▲) • [r] replay from seed • [space] pause • [n] step • [q] quit")
	return sb.String()
}

func (m flockModel) View() tea.View {
	v := tea.NewView(m.renderView())
	v.AltScreen = true
	return v
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("flock", flag.ContinueOnError)
	fs.SetOutput(stderr)
	seed := fs.Uint64("seed", 42, "deterministic world seed")
	count := fs.Int("count", 120, "number of boid actors")
	headless := fs.Bool("headless", false, "run headlessly for -steps ticks and print view")
	steps := fs.Int("steps", 40, "number of steps when -headless is set")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	m := newFlockModel(*seed, *count)
	if *headless {
		for i := 0; i < *steps; i++ {
			if i == 5 {
				m.liveDriver.PushEvent(ntactor.Event{Kind: "attractor"})
			}
			if i == 15 {
				m.liveDriver.PushEvent(ntactor.Event{Kind: "predator"})
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
