// Copyright (c) 2026 Neomantra Corp.

// Command scene3d drives a 3D nt3d scene (meshes, transforms, A* path lines,
// and a 3D Boids point cloud) from an abstract ntactor.World in a Bubble Tea
// viewport with mouse camera orbiting, ray-cast floor picking, and instant
// HistoricalDriver replay.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"os"
	"runtime"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	nt3dwidget "nimbleterminal.dev/nt3d/widget"
	"nimbleterminal.dev/ntactor"
	"nimbleterminal.dev/ntactor/examples/internal/demohelp"
	"nimbleterminal.dev/ntpicture"
)

const (
	arenaHalf    = 4.8
	gridCells    = 20
	gridCellSize = (arenaHalf * 2) / float64(gridCells)
)

var pillarCenters = []ntactor.Vec3{
	{X: -2.0, Y: -1.5, Z: 0},
	{X: 2.0, Y: -1.5, Z: 0},
	{X: -2.0, Y: 1.5, Z: 0},
	{X: 2.0, Y: 1.5, Z: 0},
}

var pillarObstacles = func() []ntactor.CircleObstacle {
	obs := make([]ntactor.CircleObstacle, len(pillarCenters))
	for i, c := range pillarCenters {
		obs[i] = ntactor.CircleObstacle{Center: c, Radius: 0.55}
	}
	return obs
}()

type role3DState struct {
	Role         string
	Intercepting bool
	Follow       *ntactor.FollowPathBehavior
	GoalID       ntactor.ActorID
}

func buildArenaGridMap() *ntactor.GridMap {
	gm := ntactor.NewGridMap(gridCells, gridCells, gridCellSize, ntactor.Vec2(-arenaHalf, -arenaHalf))
	for _, p := range pillarObstacles {
		cx, cy, ok := gm.WorldToCell(p.Center)
		if !ok {
			continue
		}
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				gm.SetWall(cx+dx, cy+dy, true)
			}
		}
	}
	return gm
}

func buildScene3DWorld(seed uint64, gm *ntactor.GridMap) *ntactor.World {
	w := ntactor.NewWorld(ntactor.Config{
		Seed:        seed,
		Perception:  ntactor.NewGridIndex(2.0),
		LineOfSight: gm.HasLineOfSight,
	})

	w.OnEvent("beacon", func(world *ntactor.World, ev ntactor.Event) {
		pos := ev.Vector
		if pos.LenSq() == 0 {
			rng := world.RNG()
			for tries := 0; tries < 16; tries++ {
				cand := ntactor.Vec3{
					X: rng.FloatRange(-arenaHalf+1.0, arenaHalf-1.0),
					Y: rng.FloatRange(-arenaHalf+1.0, arenaHalf-1.0),
					Z: 0.45,
				}
				if cx, cy, ok := gm.WorldToCell(cand); ok && !gm.IsWall(cx, cy) {
					pos = cand
					break
				}
			}
		}
		pos.Z = 0.45
		world.Spawn(ntactor.Actor{
			Tag:      "beacon",
			Position: pos,
			Behavior: ntactor.Idle,
			State:    &role3DState{Role: "beacon"},
		})
	})

	w.OnEvent("intruder", func(world *ntactor.World, _ ntactor.Event) {
		rng := world.RNG()
		pos := ntactor.Vec3{
			X: rng.FloatRange(-arenaHalf+1.0, arenaHalf-1.0),
			Y: rng.FloatRange(-arenaHalf+1.0, arenaHalf-1.0),
			Z: 0.28,
		}
		world.Spawn(ntactor.Actor{
			Tag:      "intruder",
			Position: pos,
			Velocity: rng.UnitVec2().Scale(2.4),
			Behavior: ntactor.BehaviorFunc(func(dt float64, self *ntactor.Actor, world *ntactor.World) {
				rng := world.ActorRNG(self.ID)
				desired := ntactor.SteerWander(self.Velocity, rng, 0.45, 2.6)
				avoid := ntactor.SteerAvoidCircles(self, pillarObstacles, 1.4, 3.2)
				if avoid.LenSq() > 0 {
					desired = desired.Add(avoid.Scale(1.5)).ClampLen(2.8)
				}
				if math.Abs(self.Position.X) > arenaHalf-0.8 || math.Abs(self.Position.Y) > arenaHalf-0.8 {
					toCenter := ntactor.SteerSeek(self.Position, ntactor.Vec3{Z: 0.28}, 2.6)
					desired = desired.Lerp(toCenter, 0.6)
				}
				desired.Z = 0
				ntactor.ApplySteering(self, desired, 8.0, 2.6, dt)
				self.Position.Z = 0.28
			}),
			State: &role3DState{Role: "intruder"},
		})
	})

	spawnBoidBatch := func(world *ntactor.World, count int) {
		rng := world.RNG()
		for i := 0; i < count; i++ {
			pos := ntactor.Vec3{
				X: rng.FloatRange(-3.2, 3.2),
				Y: rng.FloatRange(-3.2, 3.2),
				Z: rng.FloatRange(1.1, 2.4),
			}
			vel := ntactor.Vec3{
				X: rng.FloatRange(-2.0, 2.0),
				Y: rng.FloatRange(-2.0, 2.0),
				Z: rng.FloatRange(-0.4, 0.4),
			}
			world.Spawn(ntactor.Actor{
				Tag:      "boid",
				Position: pos,
				Velocity: vel,
				Behavior: &ntactor.FlockBehavior{
					NeighborRadius:   1.8,
					SeparationRadius: 0.7,
					SeparationWeight: 1.8,
					AlignmentWeight:  1.1,
					CohesionWeight:   1.0,
					WanderJitter:     0.15,
					MaxSpeed:         2.8,
					MaxForce:         7.5,
					SameTagOnly:      true,
					AttractTag:       "beacon",
					AttractWeight:    1.4,
					RepelTag:         "intruder",
					RepelRadius:      2.6,
					RepelWeight:      3.0,
					BoundsCenter:     ntactor.Vec3{X: 0, Y: 0, Z: 1.7},
					BoundsRadius:     3.6,
				},
			})
		}
	}

	w.OnEvent("flock", func(world *ntactor.World, _ ntactor.Event) {
		spawnBoidBatch(world, 14)
	})

	// Two FSM Perimeter Guards patrolling around the pillars and intercepting intruders.
	guardLoops := [][]ntactor.Vec3{
		{{X: -3.6, Y: -3.2, Z: 0.35}, {X: 3.6, Y: -3.2, Z: 0.35}, {X: 3.6, Y: 3.2, Z: 0.35}, {X: -3.6, Y: 3.2, Z: 0.35}},
		{{X: 3.6, Y: 3.2, Z: 0.35}, {X: -3.6, Y: 3.2, Z: 0.35}, {X: -3.6, Y: -3.2, Z: 0.35}, {X: 3.6, Y: -3.2, Z: 0.35}},
	}
	for _, wp := range guardLoops {
		patrol := &ntactor.PatrolBehavior{
			Waypoints:     wp,
			Speed:         2.4,
			ArrivalRadius: 0.45,
		}
		fsm := ntactor.NewFSM("patrol")
		fsm.AddState("patrol", ntactor.FSMState{
			OnEnter: func(self *ntactor.Actor, _ *ntactor.World) {
				self.State.(*role3DState).Intercepting = false
			},
			OnTick: func(dt float64, self *ntactor.Actor, world *ntactor.World) {
				patrol.Tick(dt, self, world)
				self.Position.Z = 0.35
			},
		})
		fsm.AddState("intercept", ntactor.FSMState{
			OnEnter: func(self *ntactor.Actor, _ *ntactor.World) {
				self.State.(*role3DState).Intercepting = true
			},
			OnTick: func(dt float64, self *ntactor.Actor, world *ntactor.World) {
				intruders := world.FindByTag("intruder")
				if len(intruders) == 0 {
					return
				}
				target := intruders[0]
				best := self.Position.Dist(target.Position)
				for _, intr := range intruders[1:] {
					if d := self.Position.Dist(intr.Position); d < best {
						best = d
						target = intr
					}
				}
				desired := ntactor.SteerPursue(self, target, 0.6, 3.2)
				avoid := ntactor.SteerAvoidCircles(self, pillarObstacles, 1.3, 3.5)
				desired = desired.Add(avoid).ClampLen(3.2)
				desired.Z = 0
				ntactor.ApplySteering(self, desired, 10.0, 3.2, dt)
				self.Position.Z = 0.35
				if best <= 0.55 {
					world.Destroy(target.ID)
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
			State:    &role3DState{Role: "guard"},
		})
	}

	// Two BehaviorTree + A* Scouts that plan smoothed paths around pillars to collect Beacons.
	scoutStarts := []ntactor.Vec3{{X: -1.0, Y: -3.4, Z: 0.28}, {X: 1.0, Y: 3.4, Z: 0.28}}
	for _, startPos := range scoutStarts {
		follow := &ntactor.FollowPathBehavior{
			Speed:         2.7,
			MaxForce:      10.0,
			ArrivalRadius: 0.35,
		}
		st := &role3DState{Role: "scout", Follow: follow}
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
					if best <= 0.55 {
						world.Destroy(target.ID)
						st.GoalID = 0
						follow.SetPath(nil)
						return ntactor.StatusSuccess
					}
					if st.GoalID != target.ID || follow.Completed() || len(follow.Path) == 0 {
						path, ok := gm.FindWorldPath(
							ntactor.Vec2(self.Position.X, self.Position.Y),
							ntactor.Vec2(target.Position.X, target.Position.Y),
							true,
						)
						if !ok || len(path) == 0 {
							path = []ntactor.Vec3{ntactor.Vec2(target.Position.X, target.Position.Y)}
						}
						for i := range path {
							path[i].Z = 0.28
						}
						follow.SetPath(path)
						st.GoalID = target.ID
					}
					follow.Tick(dt, self, world)
					self.Position.Z = 0.28
					return ntactor.StatusRunning
				}),
			),
			ntactor.Action(func(dt float64, self *ntactor.Actor, world *ntactor.World) ntactor.NodeStatus {
				st.GoalID = 0
				follow.SetPath(nil)
				rng := world.ActorRNG(self.ID)
				desired := ntactor.SteerWander(self.Velocity, rng, 0.35, 2.0)
				avoid := ntactor.SteerAvoidCircles(self, pillarObstacles, 1.3, 2.5)
				desired = desired.Add(avoid).ClampLen(2.0)
				if math.Abs(self.Position.X) > arenaHalf-1.0 || math.Abs(self.Position.Y) > arenaHalf-1.0 {
					desired = desired.Lerp(ntactor.SteerSeek(self.Position, ntactor.Vec3{Z: 0.28}, 2.0), 0.5)
				}
				desired.Z = 0
				ntactor.ApplySteering(self, desired, 7.0, 2.0, dt)
				self.Position.Z = 0.28
				return ntactor.StatusRunning
			}),
		))
		w.Spawn(ntactor.Actor{
			Tag:      "scout",
			Position: startPos,
			Behavior: bt,
			State:    st,
		})
	}

	// Spawn an initial murmuration of 42 3D Boids and one initial 3D Beacon.
	spawnBoidBatch(w, 42)
	w.Spawn(ntactor.Actor{
		Tag:      "beacon",
		Position: ntactor.Vec3{X: 0, Y: 0, Z: 0.45},
		Behavior: ntactor.Idle,
		State:    &role3DState{Role: "beacon"},
	})

	return w
}

// scene3DAdapter projects an abstract ntactor.World into an nt3d.Scene without
// ntactor knowing anything about meshes, transforms, or point clouds.
type scene3DAdapter struct {
	scene          *nt3d.Scene
	actorNodes     map[ntactor.ActorID]nt3d.NodeID
	actorStateTag  map[ntactor.ActorID]string
	boidsNode      nt3d.NodeID
	pathsNode      nt3d.NodeID
	pointsBuf      []nt3d.Point
	guardPatrolGeo nt3d.Geometry
	guardAlertGeo  nt3d.Geometry
	scoutGeo       nt3d.Geometry
	beaconGeo      nt3d.Geometry
	intruderGeo    nt3d.Geometry
}

func arenaFloorTexture() (*nt3d.Texture, error) {
	const sz = 256
	img := image.NewRGBA(image.Rect(0, 0, sz, sz))
	for y := 0; y < sz; y++ {
		for x := 0; x < sz; x++ {
			c := color.RGBA{28, 38, 52, 255}
			if (x/32+y/32)%2 == 0 {
				c = color.RGBA{34, 46, 62, 255}
			}
			if x%32 < 2 || y%32 < 2 {
				c = color.RGBA{54, 76, 102, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return nt3d.NewTexture(img)
}

func newScene3DAdapter() (*scene3DAdapter, error) {
	scene := nt3d.NewScene()
	tex, err := arenaFloorTexture()
	if err != nil {
		return nil, err
	}
	floor, err := nt3d.BuildHeightfield(nt3d.Heightfield{
		NX:       2,
		NY:       2,
		MinX:     -arenaHalf,
		MaxX:     arenaHalf,
		MinY:     -arenaHalf,
		MaxY:     arenaHalf,
		Heights:  []float32{0, 0, 0, 0},
		Material: &nt3d.Material{Texture: tex},
	})
	if err != nil {
		return nil, err
	}
	if _, err := scene.SetGeometry("floor", floor); err != nil {
		return nil, err
	}

	pillarGeo, err := nt3d.BuildCylinder(nt3d.Cylinder{
		BottomRadius: 0.48,
		TopRadius:    0.42,
		Height:       1.2,
		Segments:     20,
		Color:        color.RGBA{92, 108, 132, 255},
	})
	if err != nil {
		return nil, err
	}
	for i, p := range pillarCenters {
		id, err := scene.SetGeometry(fmt.Sprintf("pillar-%d", i+1), pillarGeo)
		if err != nil {
			return nil, err
		}
		if err := scene.SetTransform(id, nt3dmath.Translation(nt3dmath.Vec3{
			X: float32(p.X),
			Y: float32(p.Y),
			Z: 0.6,
		})); err != nil {
			return nil, err
		}
	}

	guardPatrolGeo, err := nt3d.BuildCylinder(nt3d.Cylinder{
		BottomRadius: 0.28,
		TopRadius:    0.06,
		Height:       0.7,
		Segments:     16,
		Color:        color.RGBA{78, 196, 246, 255},
	})
	if err != nil {
		return nil, err
	}
	guardAlertGeo, err := nt3d.BuildCylinder(nt3d.Cylinder{
		BottomRadius: 0.30,
		TopRadius:    0.06,
		Height:       0.75,
		Segments:     16,
		Color:        color.RGBA{255, 104, 68, 255},
	})
	if err != nil {
		return nil, err
	}
	scoutGeo, err := nt3d.BuildSphere(nt3d.Sphere{
		Radius:   0.26,
		Segments: 18,
		Rings:    12,
		Color:    color.RGBA{112, 232, 164, 255},
	})
	if err != nil {
		return nil, err
	}
	beaconGeo, err := nt3d.BuildTorus(nt3d.Torus{
		MajorRadius: 0.28,
		MinorRadius: 0.09,
		Segments:    24,
		Sides:       12,
		Color:       color.RGBA{255, 214, 74, 255},
	})
	if err != nil {
		return nil, err
	}
	intruderGeo, err := nt3d.BuildSphere(nt3d.Sphere{
		Radius:   0.26,
		Segments: 16,
		Rings:    12,
		Color:    color.RGBA{242, 72, 118, 255},
	})
	if err != nil {
		return nil, err
	}

	boidsNode, err := scene.Add(nt3d.Root, "boids-cloud")
	if err != nil {
		return nil, err
	}
	pathsNode, err := scene.Add(nt3d.Root, "scout-paths")
	if err != nil {
		return nil, err
	}

	return &scene3DAdapter{
		scene:          scene,
		actorNodes:     make(map[ntactor.ActorID]nt3d.NodeID),
		actorStateTag:  make(map[ntactor.ActorID]string),
		boidsNode:      boidsNode,
		pathsNode:      pathsNode,
		guardPatrolGeo: guardPatrolGeo,
		guardAlertGeo:  guardAlertGeo,
		scoutGeo:       scoutGeo,
		beaconGeo:      beaconGeo,
		intruderGeo:    intruderGeo,
	}, nil
}

func (a *scene3DAdapter) Sync(w *ntactor.World) {
	seen := make(map[ntactor.ActorID]bool, w.Count())
	a.pointsBuf = a.pointsBuf[:0]
	var pathLines []nt3d.Vertex

	for _, act := range w.Actors() {
		if act.Tag == "boid" {
			spd := act.Velocity.Len()
			t := math.Min(1.0, spd/3.0)
			a.pointsBuf = append(a.pointsBuf, nt3d.Point{
				Position: nt3dmath.Vec3{
					X: float32(act.Position.X),
					Y: float32(act.Position.Y),
					Z: float32(act.Position.Z),
				},
				Radius: 0.11,
				Color: color.RGBA{
					R: uint8(110 + 135*t),
					G: uint8(195 + 50*(1-t)),
					B: 255,
					A: 230,
				},
			})
			continue
		}

		seen[act.ID] = true
		st, _ := act.State.(*role3DState)
		variant := act.Tag
		if act.Tag == "guard" && st != nil && st.Intercepting {
			variant = "guard:alert"
		}

		node, exists := a.actorNodes[act.ID]
		if !exists {
			var geo nt3d.Geometry
			switch variant {
			case "guard:alert":
				geo = a.guardAlertGeo
			case "guard":
				geo = a.guardPatrolGeo
			case "scout":
				geo = a.scoutGeo
			case "beacon":
				geo = a.beaconGeo
			default:
				geo = a.intruderGeo
			}
			var err error
			node, err = a.scene.SetGeometry(fmt.Sprintf("actor-%d", act.ID), geo)
			if err != nil {
				continue
			}
			a.actorNodes[act.ID] = node
			a.actorStateTag[act.ID] = variant
		} else if a.actorStateTag[act.ID] != variant {
			geo := a.guardPatrolGeo
			if variant == "guard:alert" {
				geo = a.guardAlertGeo
			}
			_ = a.scene.SetNodeGeometry(node, geo)
			a.actorStateTag[act.ID] = variant
		}

		pos := nt3dmath.Vec3{
			X: float32(act.Position.X),
			Y: float32(act.Position.Y),
			Z: float32(act.Position.Z),
		}
		if act.Tag == "beacon" {
			pos.Z += float32(0.08 * math.Sin(w.SimTime()*3.5+float64(act.ID)))
			spin := w.SimTime()*2.2 + float64(act.ID)
			_ = a.scene.SetTransform(node, nt3dmath.Translation(pos).Mul(nt3dmath.RotationZ(spin)))
		} else {
			yaw := 0.0
			if act.Velocity.LenSq() > 1e-4 {
				yaw = math.Atan2(act.Velocity.Y, act.Velocity.X)
			}
			_ = a.scene.SetTransform(node, nt3dmath.Translation(pos).Mul(nt3dmath.RotationZ(yaw)))
		}

		if act.Tag == "scout" && st != nil && st.Follow != nil && len(st.Follow.Path) > 0 {
			lineColor := color.RGBA{112, 232, 164, 255}
			prev := nt3dmath.Vec3{X: float32(act.Position.X), Y: float32(act.Position.Y), Z: 0.06}
			for i := st.Follow.CurrentIndex; i < len(st.Follow.Path); i++ {
				wp := st.Follow.Path[i]
				next := nt3dmath.Vec3{X: float32(wp.X), Y: float32(wp.Y), Z: 0.06}
				pathLines = append(pathLines,
					nt3d.Vertex{Position: prev, Color: lineColor},
					nt3d.Vertex{Position: next, Color: lineColor},
				)
				prev = next
			}
		}
	}

	for id, node := range a.actorNodes {
		if !seen[id] {
			_ = a.scene.Remove(node)
			delete(a.actorNodes, id)
			delete(a.actorStateTag, id)
		}
	}

	_ = a.scene.SetNodeGeometry(a.boidsNode, nt3d.Geometry{
		PointStyle: nt3d.PointStyle{
			Shape: nt3d.SoftPoint,
			Units: nt3d.WorldUnits,
			Blend: nt3d.AlphaBlend,
		},
		Points: a.pointsBuf,
	})
	_ = a.scene.SetNodeGeometry(a.pathsNode, nt3d.Geometry{
		Lines:     pathLines,
		LineWidth: 2,
	})
}

type tickMsg struct{}

func tickCmd() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg {
		return tickMsg{}
	})
}

type scene3DModel struct {
	seed       uint64
	gridMap    *ntactor.GridMap
	world      *ntactor.World
	liveDriver *ntactor.LiveDriver
	histDriver *ntactor.HistoricalDriver
	adapter    *scene3DAdapter
	view       *nt3dwidget.Model
	zones      *zone.Manager
	replaying  bool
	paused     bool
	closed     bool
}

func newScene3DModel(seed uint64) (*scene3DModel, error) {
	gm := buildArenaGridMap()
	w := buildScene3DWorld(seed, gm)
	live := ntactor.NewLiveDriver(ntactor.LiveConfig{
		Seed:      seed,
		DefaultDT: 0.05,
		Record:    true,
	})
	adapter, err := newScene3DAdapter()
	if err != nil {
		return nil, err
	}
	adapter.Sync(w)

	zones := zone.New()
	cam := nt3d.DefaultCamera()
	cam.Alpha = 32
	cam.Beta = 35
	cam.Distance = 12.5
	cam.Projection = nt3d.Perspective

	transport := ntpicture.KittyMediumDirect
	if runtime.GOOS == "js" {
		transport = ntpicture.KittyMediumSharedMemory
	}

	view := nt3dwidget.New(
		adapter.scene,
		88,
		22,
		nt3dwidget.WithCamera(cam),
		nt3dwidget.WithBackground(color.RGBA{15, 22, 32, 255}),
		nt3dwidget.WithGPUFailure(nt3dwidget.FallBackOnGPUFailure),
		nt3dwidget.WithKittyMedium(transport),
		nt3dwidget.WithZoneManager(zones),
	)

	return &scene3DModel{
		seed:       seed,
		gridMap:    gm,
		world:      w,
		liveDriver: live,
		adapter:    adapter,
		view:       view,
		zones:      zones,
	}, nil
}

func (m *scene3DModel) close() {
	if m.view != nil {
		_ = m.view.Close()
	}
	if m.zones != nil {
		m.zones.Close()
	}
}

func (m *scene3DModel) stepOnce() {
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
	m.adapter.Sync(m.world)
}

func (m *scene3DModel) Init() tea.Cmd {
	return tea.Batch(m.view.Init(), tickCmd())
}

func (m *scene3DModel) Help() []key.Binding {
	return []key.Binding{
		demohelp.Key("Beacon", "b"),
		demohelp.Key("Intruder", "i"),
		demohelp.Key("Flock boids", "f"),
		demohelp.Key("Replay log", "r"),
		demohelp.Key("Renderer", "m"),
		demohelp.Key("Pause", "space", " "),
		demohelp.Key("Step", "n"),
		demohelp.Key("Reset", "x"),
		demohelp.Key("Quit", "q", "ctrl+c"),
	}
}

func (m *scene3DModel) Summary() string {
	mode := "LIVE"
	if m.replaying && m.histDriver != nil {
		mode = fmt.Sprintf("REPLAY %d/%d", m.histDriver.Index(), m.histDriver.Len())
	} else if m.paused {
		mode = "PAUSED"
	}
	return fmt.Sprintf("Scene3D [%s]: tick=%d actors=%d boids=%d beacons=%d intruders=%d renderer=%s",
		mode,
		m.world.TickCount(),
		m.world.Count(),
		len(m.world.FindByTag("boid")),
		len(m.world.FindByTag("beacon")),
		len(m.world.FindByTag("intruder")),
		m.view.RenderMode(),
	)
}

func (m *scene3DModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		if m.closed {
			return m, nil
		}
		if !m.paused {
			m.stepOnce()
			if !m.view.Busy() {
				return m, tea.Batch(m.view.Refresh(), tickCmd())
			}
		}
		return m, tickCmd()

	case tea.WindowSizeMsg:
		return m, m.view.SetSize(msg.Width, max(1, msg.Height-3))

	case nt3dwidget.PickMsg:
		// Ray-cast click onto the Z=0 arena floor to spawn a 3D Beacon.
		if !m.replaying && math.Abs(float64(msg.Ray.Direction.Z)) > 1e-4 {
			t := -msg.Ray.Origin.Z / msg.Ray.Direction.Z
			if t > 0 {
				hit := msg.Ray.Origin.Add(msg.Ray.Direction.Scale(t))
				if math.Abs(float64(hit.X)) <= arenaHalf-0.5 && math.Abs(float64(hit.Y)) <= arenaHalf-0.5 {
					m.liveDriver.PushEvent(ntactor.Event{
						Kind:   "beacon",
						Vector: ntactor.Vec3{X: float64(hit.X), Y: float64(hit.Y), Z: 0.45},
					})
					m.stepOnce()
					return m, m.view.Refresh()
				}
			}
		}

	case tea.KeyMsg:
		if demohelp.Quit(msg) {
			m.closed = true
			return m, tea.Sequence(m.view.Dispose(), tea.Quit)
		}
		switch msg.String() {
		case "space", " ":
			m.paused = !m.paused
			return m, nil
		case "n":
			m.stepOnce()
			return m, m.view.Refresh()
		case "b":
			if !m.replaying {
				m.liveDriver.PushEvent(ntactor.Event{Kind: "beacon"})
				m.stepOnce()
				return m, m.view.Refresh()
			}
		case "i":
			if !m.replaying {
				m.liveDriver.PushEvent(ntactor.Event{Kind: "intruder"})
				m.stepOnce()
				return m, m.view.Refresh()
			}
		case "f":
			if !m.replaying {
				m.liveDriver.PushEvent(ntactor.Event{Kind: "flock"})
				m.stepOnce()
				return m, m.view.Refresh()
			}
		case "r":
			log := m.liveDriver.Log()
			if log.Len() > 0 {
				m.world = buildScene3DWorld(log.Seed, m.gridMap)
				m.histDriver = ntactor.NewHistoricalDriver(log)
				m.replaying = true
				m.paused = false
				m.adapter.Sync(m.world)
				return m, m.view.Refresh()
			}
			return m, nil
		case "m":
			next := (m.view.RequestedRenderMode() + 1) % 3
			return m, m.view.SetRenderMode(next)
		case "x":
			m.world = buildScene3DWorld(m.seed, m.gridMap)
			m.liveDriver = ntactor.NewLiveDriver(ntactor.LiveConfig{
				Seed:      m.seed,
				DefaultDT: 0.05,
				Record:    true,
			})
			m.histDriver = nil
			m.replaying = false
			m.paused = false
			m.adapter.Sync(m.world)
			return m, m.view.Refresh()
		}
	}

	_, cmd := m.view.Update(msg)
	return m, cmd
}

func (m *scene3DModel) View() tea.View {
	v := m.view.View()
	header := "ntactor × nt3d — 3D Arena (FSM Cone Guards · A* Sphere Scouts · Torus Beacons · 3D Boids Point Cloud)"
	footer := fmt.Sprintf("%s  |  drag/hjkl: orbit · wheel/+/-: zoom · click floor/b: beacon · i: intruder · f: boids · m: renderer · r: replay · q: quit",
		m.Summary())
	v.Content = m.zones.Scan(header + "\n" + v.Content + "\n" + footer)
	v.AltScreen = true
	return v
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("scene3d", flag.ContinueOnError)
	fs.SetOutput(stderr)
	seed := fs.Uint64("seed", 42, "deterministic world seed")
	headless := fs.Bool("headless", false, "run headlessly for -steps ticks, render a software snapshot, and print summary")
	steps := fs.Int("steps", 30, "number of steps when -headless is set")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	m, err := newScene3DModel(*seed)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer m.close()

	if *headless {
		_ = m.view.SetRenderMode(nt3d.Software)
		for i := 0; i < *steps; i++ {
			switch i {
			case 5:
				m.liveDriver.PushEvent(ntactor.Event{Kind: "beacon"})
			case 10:
				m.liveDriver.PushEvent(ntactor.Event{Kind: "intruder"})
			case 15:
				m.liveDriver.PushEvent(ntactor.Event{Kind: "flock"})
			}
			m.stepOnce()
		}
		img, usedMode, err := m.view.Snapshot(320, 200)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		b := img.Bounds()
		var out strings.Builder
		fmt.Fprintf(&out, "%s\nSnapshot: %dx%d rendered via %s | worldHash=0x%016x\n",
			m.Summary(), b.Dx(), b.Dy(), usedMode, m.world.Hash())
		fmt.Fprint(stdout, out.String())
		return 0
	}

	if err := demohelp.Run(m); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
