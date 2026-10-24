// Copyright (c) 2026 Neomantra Corp.

// Command planner demonstrates declarative JSON BehaviorSpecs, BehaviorRegistry,
// Utility AI, Actor Blackboards, counterfactual rollout ranking (CompareRollouts),
// and deterministic EventLog hot-swapping.
package main

import (
	"encoding/json"
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
	plannerW = 48
	plannerH = 16
)

type presetSpec struct {
	Name        string
	Description string
	Spec        ntactor.BehaviorSpec
	JSON        string
}

func buildCandidateSpecs() []presetSpec {
	rawSpecs := []struct {
		name string
		desc string
		spec ntactor.BehaviorSpec
	}{
		{
			name: "utility_hunter",
			desc: "UtilitySelector: scores intercept_intruder vs A* collect_beacon vs patrol_box",
			spec: ntactor.BehaviorSpec{
				Type: "utility",
				Options: []ntactor.UtilityOptionSpec{
					{
						Name:   "intercept_intruder",
						Weight: 1.3,
						Considerations: []ntactor.ConsiderationSpec{
							{
								Name:   "nearest_tag_distance",
								Params: map[string]any{"tag": "intruder", "max_radius": 35.0},
								Min:    0,
								Max:    35,
								Curve:  ntactor.ResponseCurve{Kind: ntactor.CurveLinear, Invert: true},
							},
						},
						Child: &ntactor.BehaviorSpec{
							Type: "action",
							Name: "pursue_tag",
							Params: map[string]any{
								"tag":            "intruder",
								"speed":          10.5,
								"capture_radius": 1.3,
								"destroy_target": true,
								"score_key":      "captures",
								"intent":         "captured_intruder",
							},
						},
					},
					{
						Name:   "collect_beacon",
						Weight: 1.0,
						Considerations: []ntactor.ConsiderationSpec{
							{
								Name:   "nearest_tag_distance",
								Params: map[string]any{"tag": "beacon", "max_radius": 40.0},
								Min:    0,
								Max:    40,
								Curve:  ntactor.ResponseCurve{Kind: ntactor.CurveLinear, Invert: true},
							},
						},
						Child: &ntactor.BehaviorSpec{
							Type: "action",
							Name: "follow_path_to_tag",
							Params: map[string]any{
								"tag":            "beacon",
								"speed":          9.0,
								"smooth":         true,
								"capture_radius": 1.2,
								"destroy_target": true,
								"score_key":      "beacons",
								"intent":         "collected_beacon",
							},
						},
					},
					{
						Name:   "patrol_perimeter",
						Weight: 0.25,
						Child: &ntactor.BehaviorSpec{
							Type: "action",
							Name: "patrol_box",
							Params: map[string]any{
								"half_width":  16.0,
								"half_height": 4.5,
								"speed":       7.0,
							},
						},
					},
				},
			},
		},
		{
			name: "astar_collector",
			desc: "Selector: plans smoothed A* paths to Beacons, else wanders",
			spec: ntactor.BehaviorSpec{
				Type: "selector",
				Children: []ntactor.BehaviorSpec{
					{
						Type: "sequence",
						Children: []ntactor.BehaviorSpec{
							{
								Type:   "condition",
								Name:   "tag_within_radius",
								Params: map[string]any{"tag": "beacon", "radius": 60.0},
							},
							{
								Type: "action",
								Name: "follow_path_to_tag",
								Params: map[string]any{
									"tag":            "beacon",
									"speed":          9.5,
									"smooth":         true,
									"capture_radius": 1.2,
									"destroy_target": true,
									"score_key":      "beacons",
									"intent":         "collected_beacon",
								},
							},
						},
					},
					{
						Type:   "action",
						Name:   "wander",
						Params: map[string]any{"speed": 6.0, "jitter": 0.4, "max_radius": 12.0},
					},
				},
			},
		},
		{
			name: "interceptor_guard",
			desc: "Selector: pursues Intruders with lead prediction, else patrols perimeter",
			spec: ntactor.BehaviorSpec{
				Type: "selector",
				Children: []ntactor.BehaviorSpec{
					{
						Type: "sequence",
						Children: []ntactor.BehaviorSpec{
							{
								Type:   "condition",
								Name:   "tag_within_radius",
								Params: map[string]any{"tag": "intruder", "radius": 60.0},
							},
							{
								Type: "action",
								Name: "pursue_tag",
								Params: map[string]any{
									"tag":            "intruder",
									"speed":          11.0,
									"capture_radius": 1.3,
									"destroy_target": true,
									"score_key":      "captures",
									"intent":         "captured_intruder",
								},
							},
						},
					},
					{
						Type: "action",
						Name: "patrol_box",
						Params: map[string]any{
							"half_width":  16.0,
							"half_height": 4.5,
							"speed":       7.5,
						},
					},
				},
			},
		},
		{
			name: "cautious_evader",
			desc: "Selector: evades nearby Intruders, else seeks Beacons, else wanders",
			spec: ntactor.BehaviorSpec{
				Type: "selector",
				Children: []ntactor.BehaviorSpec{
					{
						Type:   "action",
						Name:   "evade_tag",
						Params: map[string]any{"tag": "intruder", "radius": 9.0, "speed": 9.5},
					},
					{
						Type: "action",
						Name: "arrive_tag",
						Params: map[string]any{
							"tag":            "beacon",
							"speed":          8.0,
							"capture_radius": 1.2,
							"destroy_target": true,
							"score_key":      "beacons",
							"intent":         "collected_beacon",
						},
					},
					{
						Type:   "action",
						Name:   "wander",
						Params: map[string]any{"speed": 5.5, "jitter": 0.35, "max_radius": 10.0},
					},
				},
			},
		},
	}

	out := make([]presetSpec, len(rawSpecs))
	for i, s := range rawSpecs {
		b, err := json.MarshalIndent(s.spec, "", "  ")
		if err != nil {
			panic(err)
		}
		out[i] = presetSpec{
			Name:        s.name,
			Description: s.desc,
			Spec:        s.spec,
			JSON:        string(b),
		}
	}
	return out
}

func buildPlannerGridMap() *ntactor.GridMap {
	gm := ntactor.NewGridMap(plannerW, plannerH, 1.0, ntactor.Vec2(-float64(plannerW)/2, -float64(plannerH)/2))
	// Two interior baffle walls that A* pathfinding routes around.
	gm.SetRectWall(16, 3, 16, 10, true)
	gm.SetRectWall(32, 5, 32, 12, true)
	return gm
}

func buildPlannerWorld(seed uint64, gm *ntactor.GridMap, reg *ntactor.BehaviorRegistry, initialSpec ntactor.BehaviorSpec) *ntactor.World {
	w := ntactor.NewWorld(ntactor.Config{
		Seed:        seed,
		Perception:  ntactor.NewGridIndex(3.5),
		LineOfSight: gm.HasLineOfSight,
	})
	reg.RegisterSwapHandler(w, "set_behavior")

	w.OnEvent("beacon", func(world *ntactor.World, _ ntactor.Event) {
		rng := world.RNG()
		for tries := 0; tries < 20; tries++ {
			pos := ntactor.Vec2(
				rng.FloatRange(-float64(plannerW)/2+4, float64(plannerW)/2-4),
				rng.FloatRange(-float64(plannerH)/2+2, float64(plannerH)/2-2),
			)
			if cx, cy, ok := gm.WorldToCell(pos); ok && !gm.IsWall(cx, cy) {
				world.Spawn(ntactor.Actor{Tag: "beacon", Position: pos, Behavior: ntactor.Idle})
				return
			}
		}
	})

	w.OnEvent("intruder", func(world *ntactor.World, _ ntactor.Event) {
		rng := world.RNG()
		pos := ntactor.Vec2(
			rng.FloatRange(-float64(plannerW)/2+4, float64(plannerW)/2-4),
			rng.FloatRange(-float64(plannerH)/2+2, float64(plannerH)/2-2),
		)
		world.Spawn(ntactor.Actor{
			Tag:      "intruder",
			Position: pos,
			Behavior: &ntactor.WanderBehavior{
				Speed:     6.5,
				Jitter:    0.45,
				MaxRadius: float64(plannerH)/2 - 2,
			},
		})
	})

	// Spawn two programmable Units initialized with initialSpec.
	starts := []ntactor.Vec3{ntactor.Vec2(-12, -3), ntactor.Vec2(12, 3)}
	for _, pos := range starts {
		bt, err := reg.Compile(initialSpec)
		if err != nil {
			panic(err)
		}
		id := w.Spawn(ntactor.Actor{
			Tag:      "unit",
			Position: pos,
			Behavior: bt,
		})
		if u := w.ActorByID(id); u != nil {
			u.Blackboard().SetString("spec_name", "utility_hunter")
		}
	}

	// Spawn initial targets so rollouts and live runs have immediate tasks.
	w.Spawn(ntactor.Actor{Tag: "beacon", Position: ntactor.Vec2(-4, 4), Behavior: ntactor.Idle})
	w.Spawn(ntactor.Actor{Tag: "beacon", Position: ntactor.Vec2(15, -4), Behavior: ntactor.Idle})
	w.Spawn(ntactor.Actor{Tag: "intruder", Position: ntactor.Vec2(3, -3), Behavior: &ntactor.WanderBehavior{
		Speed: 6.0, Jitter: 0.4, MaxRadius: 5.5,
	}})

	return w
}

func syncPlannerCanvas(grid *ntcanvas.GraphemeGrid, w *ntactor.World, gm *ntactor.GridMap, specName, mode string) {
	grid.Clear()
	dim := ntcanvas.CellStyle{Attributes: ntcanvas.Dim}
	bold := ntcanvas.CellStyle{Attributes: ntcanvas.Bold}

	for x := 0; x < plannerW; x++ {
		grid.SetTextWithStyle(ntcanvas.Point{X: x, Y: 0}, "─", dim)
		grid.SetTextWithStyle(ntcanvas.Point{X: x, Y: plannerH - 1}, "─", dim)
	}
	for y := 0; y < plannerH; y++ {
		grid.SetTextWithStyle(ntcanvas.Point{X: 0, Y: y}, "│", dim)
		grid.SetTextWithStyle(ntcanvas.Point{X: plannerW - 1, Y: y}, "│", dim)
	}
	grid.SetTextWithStyle(ntcanvas.Point{X: 0, Y: 0}, "┌", dim)
	grid.SetTextWithStyle(ntcanvas.Point{X: plannerW - 1, Y: 0}, "┐", dim)
	grid.SetTextWithStyle(ntcanvas.Point{X: 0, Y: plannerH - 1}, "└", dim)
	grid.SetTextWithStyle(ntcanvas.Point{X: plannerW - 1, Y: plannerH - 1}, "┘", dim)

	for y := 1; y < plannerH-1; y++ {
		for x := 1; x < plannerW-1; x++ {
			if gm.IsWall(x, y) {
				grid.SetTextWithStyle(ntcanvas.Point{X: x, Y: y}, "█", dim)
			}
		}
	}

	for _, a := range w.Actors() {
		gx := int(math.Round(a.Position.X)) + plannerW/2
		gy := int(math.Round(a.Position.Y)) + plannerH/2
		if gx < 1 || gx >= plannerW-1 || gy < 1 || gy >= plannerH-1 {
			continue
		}
		switch a.Tag {
		case "unit":
			grid.SetTextWithStyle(ntcanvas.Point{X: gx, Y: gy}, "U", bold)
		case "beacon":
			grid.SetTextWithStyle(ntcanvas.Point{X: gx, Y: gy}, "✦", bold)
		case "intruder":
			grid.SetTextWithStyle(ntcanvas.Point{X: gx, Y: gy}, "!", bold)
		}
	}

	header := fmt.Sprintf(" [%s] spec=%s tick=%d ", mode, specName, w.TickCount())
	grid.SetTextWithStyle(ntcanvas.Point{X: 2, Y: 0}, header, bold)
}

type tickMsg struct{}

func tickCmd() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg {
		return tickMsg{}
	})
}

type plannerModel struct {
	seed       uint64
	gm         *ntactor.GridMap
	reg        *ntactor.BehaviorRegistry
	presets    []presetSpec
	specIdx    int
	world      *ntactor.World
	liveDriver *ntactor.LiveDriver
	histDriver *ntactor.HistoricalDriver
	rankings   []ntactor.RankedCandidate
	replaying  bool
	paused     bool
	grid       *ntcanvas.GraphemeGrid
	canvas     ntcanvas.Model
}

func newPlannerModel(seed uint64) plannerModel {
	grid, err := ntcanvas.NewGraphemeGridChecked(plannerW, plannerH, ntcanvas.SizeLimits{})
	if err != nil {
		panic(err)
	}
	gm := buildPlannerGridMap()
	reg := ntactor.NewStandardRegistry(ntactor.StandardRegistryConfig{GridMap: gm})
	presets := buildCandidateSpecs()
	w := buildPlannerWorld(seed, gm, reg, presets[0].Spec)
	live := ntactor.NewLiveDriver(ntactor.LiveConfig{
		Seed:      seed,
		DefaultDT: 0.05,
		Record:    true,
	})
	c := ntcanvas.NewModel(grid, ntcanvas.WithFocus(),
		ntcanvas.WithViewWidth(plannerW), ntcanvas.WithViewHeight(plannerH))
	m := plannerModel{
		seed:       seed,
		gm:         gm,
		reg:        reg,
		presets:    presets,
		specIdx:    0,
		world:      w,
		liveDriver: live,
		grid:       grid,
		canvas:     c,
	}
	m.runRolloutComparison()
	syncPlannerCanvas(grid, w, gm, presets[0].Name, "LIVE")
	return m
}

func (m *plannerModel) runRolloutComparison() {
	candidates := make(map[string]ntactor.BehaviorSpec, len(m.presets))
	for _, p := range m.presets {
		candidates[p.Name] = p.Spec
	}
	ranked, err := ntactor.CompareRollouts(ntactor.RolloutConfig{
		BuildWorld: func(seed uint64) *ntactor.World {
			return buildPlannerWorld(seed, m.gm, m.reg, m.presets[0].Spec)
		},
		Registry:  m.reg,
		Seed:      m.seed + m.world.TickCount(),
		TargetTag: "unit",
		Ticks:     140,
		DT:        0.05,
		Score: func(w *ntactor.World, _ []ntactor.IntentSummary) float64 {
			var total float64
			for _, u := range w.FindByTag("unit") {
				total += u.Blackboard().FloatOr("captures", 0)*15.0 + u.Blackboard().FloatOr("beacons", 0)*10.0
			}
			return total
		},
	}, candidates)
	if err == nil {
		m.rankings = ranked
	}
}

func (m *plannerModel) hotSwapSpec(idx int) {
	m.specIdx = ((idx % len(m.presets)) + len(m.presets)) % len(m.presets)
	chosen := m.presets[m.specIdx]
	for _, u := range m.world.FindByTag("unit") {
		m.liveDriver.PushEvent(ntactor.Event{
			Kind:    "set_behavior",
			Target:  u.ID,
			Payload: chosen.JSON,
		})
		u.Blackboard().SetString("spec_name", chosen.Name)
	}
	m.stepOnce()
}

func (m *plannerModel) stepOnce() {
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
	mode := "LIVE"
	if m.replaying && m.histDriver != nil {
		mode = fmt.Sprintf("REPLAY %d/%d", m.histDriver.Index(), m.histDriver.Len())
	} else if m.paused {
		mode = "PAUSED"
	}
	syncPlannerCanvas(m.grid, m.world, m.gm, m.presets[m.specIdx].Name, mode)
}

func (m plannerModel) Init() tea.Cmd {
	return tickCmd()
}

func (m plannerModel) Help() []key.Binding {
	return []key.Binding{
		demohelp.Key("Next spec", "s", "tab"),
		demohelp.Key("Eval rollouts", "e"),
		demohelp.Key("Beacon", "b"),
		demohelp.Key("Intruder", "i"),
		demohelp.Key("Replay log", "r"),
		demohelp.Key("Pause", "space", " "),
		demohelp.Key("Step", "n"),
		demohelp.Key("Reset", "x"),
		demohelp.Key("Quit", "q", "ctrl+c"),
	}
}

func (m plannerModel) Summary() string {
	mode := "LIVE"
	if m.replaying && m.histDriver != nil {
		mode = fmt.Sprintf("REPLAY %d/%d", m.histDriver.Index(), m.histDriver.Len())
	} else if m.paused {
		mode = "PAUSED"
	}
	topWinner := "n/a"
	if len(m.rankings) > 0 {
		topWinner = fmt.Sprintf("%s(%.0f)", m.rankings[0].Name, m.rankings[0].Result.Score)
	}
	return fmt.Sprintf("Planner [%s]: spec=%s tick=%d actors=%d bestRollout=%s hash=0x%016x",
		mode, m.presets[m.specIdx].Name, m.world.TickCount(), m.world.Count(), topWinner, m.world.Hash())
}

func (m plannerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		case "s", "tab":
			if !m.replaying {
				m.hotSwapSpec(m.specIdx + 1)
			}
		case "e":
			if !m.replaying {
				m.runRolloutComparison()
				if len(m.rankings) > 0 {
					winName := m.rankings[0].Name
					for i, p := range m.presets {
						if p.Name == winName {
							m.hotSwapSpec(i)
							break
						}
					}
				}
			}
		case "b":
			if !m.replaying {
				m.liveDriver.PushEvent(ntactor.Event{Kind: "beacon"})
				m.stepOnce()
			}
		case "i":
			if !m.replaying {
				m.liveDriver.PushEvent(ntactor.Event{Kind: "intruder"})
				m.stepOnce()
			}
		case "r":
			log := m.liveDriver.Log()
			if log.Len() > 0 {
				m.world = buildPlannerWorld(log.Seed, m.gm, m.reg, m.presets[0].Spec)
				m.histDriver = ntactor.NewHistoricalDriver(log)
				m.replaying = true
				m.paused = false
			}
		case "x":
			m = newPlannerModel(m.seed)
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.canvas, cmd = m.canvas.Update(msg)
	return m, cmd
}

func (m plannerModel) renderView() string {
	canvasLines := strings.Split(m.canvas.View(), "\n")
	activePreset := m.presets[m.specIdx]
	cat := m.reg.Catalog()

	var right []string
	right = append(right,
		fmt.Sprintf("DECLARATIVE BEHAVIOR SPEC (%d/%d): %s", m.specIdx+1, len(m.presets), activePreset.Name),
		fmt.Sprintf("  %s", activePreset.Description),
		fmt.Sprintf("  Registry Catalog: %d conditions · %d actions · %d considerations",
			len(cat.Conditions), len(cat.Actions), len(cat.Considerations)),
		"",
		"COUNTERFACTUAL ROLLOUT RANKING (CompareRollouts, 140 ticks):",
	)
	for i, rk := range m.rankings {
		marker := " "
		if rk.Name == activePreset.Name {
			marker = "▸"
		}
		right = append(right, fmt.Sprintf("  %s #%d %-18s score=%5.1f  hash=0x%08x",
			marker, i+1, rk.Name, rk.Result.Score, uint32(rk.Result.WorldHash)))
	}

	right = append(right, "", "UNIT BLACKBOARDS (actor.Blackboard()):")
	for _, u := range m.world.FindByTag("unit") {
		bb := u.Blackboard()
		choice := bb.StringOr("utility_choice", "-")
		status := bb.StringOr("status", "idle")
		caps := int(bb.FloatOr("captures", 0))
		beac := int(bb.FloatOr("beacons", 0))
		right = append(right, fmt.Sprintf("  Unit #%d: status=%-18s utility=%-18s captures=%d beacons=%d",
			u.ID, status, choice, caps, beac))
	}

	var sb strings.Builder
	sb.WriteString("ntactor Declarative Behavior Planner — JSON BehaviorSpec + Utility AI + Rollout Evaluator + Blackboard\n")
	sb.WriteString("Legend: U=Programmable Unit  ✦=Beacon  !=Intruder  █=Wall  |  Specs hot-swap via Event{Kind: \"set_behavior\", Payload: json}\n\n")

	total := len(canvasLines)
	if len(right) > total {
		total = len(right)
	}
	for i := 0; i < total; i++ {
		left := strings.Repeat(" ", plannerW)
		if i < len(canvasLines) {
			left = canvasLines[i]
		}
		sb.WriteString(left)
		if i < len(right) && right[i] != "" {
			sb.WriteString("   ")
			sb.WriteString(right[i])
		}
		sb.WriteByte('\n')
	}

	jsonLines := strings.Split(activePreset.JSON, "\n")
	maxJSONLines := 8
	if len(jsonLines) > maxJSONLines {
		jsonLines = append(jsonLines[:maxJSONLines], "  ... (compiled via BehaviorRegistry.CompileJSON)")
	}
	sb.WriteString("\nACTIVE SPEC JSON:\n")
	for _, jl := range jsonLines {
		sb.WriteString("  ")
		sb.WriteString(jl)
		sb.WriteByte('\n')
	}

	sb.WriteString("\nCONTROLS: [s] hot-swap spec • [e] evaluate rollouts & deploy best • [b] beacon • [i] intruder • [r] replay • [space] pause • [q] quit")
	return sb.String()
}

func (m plannerModel) View() tea.View {
	v := tea.NewView(m.renderView())
	v.AltScreen = true
	return v
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("planner", flag.ContinueOnError)
	fs.SetOutput(stderr)
	seed := fs.Uint64("seed", 42, "deterministic world seed")
	headless := fs.Bool("headless", false, "run headlessly for -steps ticks and print view")
	steps := fs.Int("steps", 40, "number of steps when -headless is set")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	m := newPlannerModel(*seed)
	if *headless {
		for i := 0; i < *steps; i++ {
			if i == 10 {
				m.hotSwapSpec(1)
			}
			if i == 20 {
				m.liveDriver.PushEvent(ntactor.Event{Kind: "beacon"})
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
