# `ntactor` Developer Guide

Import `nimbleterminal.dev/ntactor`; its package name is `ntactor`.

`ntactor` is a pure-Go, zero-dependency library for classical game and simulation **Actors** and their **Behaviors**. Because the root module never calls wall-clock APIs (`time.Now`), never uses global random state, and never iterates over Go `map` values during ticks or spatial queries, any simulation seeded with the same `uint64` and fed the same `(dt, events)` frames produces the exact same `World.Hash()` across `linux`, `darwin`, `windows`, `freebsd`, and `js/wasm`.

---

## 1. The Simulation Loop & Adapter Boundary

An `ntactor.World` owns only simulation time, seeded randomness, spatial perception, and a dense slice of active `Actor` structs sorted in ascending `ActorID` order. It knows nothing about terminal cells, 3D scene-graph nodes, meshes, or pixels.

### Order of Operations Inside `World.Tick(dt, events)`

Every call to `world.Tick(dt, events)` executes five deterministic phases:

1. **Clear prior tick's Intents** (`world.Intents()` is reset).
2. **Advance `SimTime`** (`simTime += dt`, `tickCount++`).
3. **Dispatch external `Event` values** in slice order to registered `world.OnEvent(kind, handler)` callbacks (and flush any `Spawn`/`Destroy` queued by event handlers).
4. **Rebuild the `SpatialIndex`** (such as `NewGridIndex(cellSize)`) over all active Actors.
5. **Tick every active `Actor.Behavior`** in ascending `ActorID` order, then flush any `Spawn`/`Destroy` calls made during Actor ticks.

```go
world := ntactor.NewWorld(ntactor.Config{
    Seed:        42,
    Perception:  ntactor.NewGridIndex(4.0), // uniform 2D/3D spatial hash
    LineOfSight: gridMap.HasLineOfSight,    // optional LOS predicate
})

// Register external event handlers once during setup:
world.OnEvent("spawn_beacon", func(w *ntactor.World, ev ntactor.Event) {
    w.Spawn(ntactor.Actor{
        Tag:      "beacon",
        Position: ev.Vector,
        Behavior: ntactor.Idle,
    })
})

// Step the simulation:
world.Tick(1.0/60.0, events)

// External adapters read world.Actors() and world.Intents() after Tick:
for _, a := range world.Actors() {
    // Project a.Position / a.Velocity onto ntcanvas, nt3d, or a point-cloud slice
}
for _, intent := range world.Intents() {
    // Handle game rules, sound effects, UI notifications, or particle bursts
}
```

### The Three Rules of Determinism

When writing custom `Behavior` implementations, follow three rules to preserve bit-exact replay and `World.Hash()` determinism:

1. **Use `dt`, never `time.Now()`** — accumulate timers using the `dt` argument passed to `Tick(dt, self, world)` or read `world.SimTime()`.
2. **Use `world.ActorRNG(self.ID)` or `world.RNG()`** — never call `math/rand` global functions. `world.ActorRNG(self.ID)` derives an independent `xoshiro256**` stream per Actor so adding or removing other Actors does not perturb `self`'s random draws.
3. **Never iterate over Go `map` values** if iteration order affects kinematics, state, or RNG draws. Always use `world.Actors()`, `world.FindByTag(tag)`, `world.Neighbors(id, radius)`, `world.ActorsWithinRadius(center, radius)`, or `self.Blackboard()`, all of which maintain deterministic ordering.

---

## 2. Actor State: Typed `State` vs. `Blackboard`

Each `Actor` provides two ways to store per-Actor state:

- **`Actor.State` (`any`)** — best for custom Go struct pointers in hand-written behaviors where static type assertions are convenient.
- **`Actor.Blackboard()` and `World.Blackboard()` (`*Blackboard`)** — a sorted, zero-map-iteration key-value store supporting `Float`, `Int`, `Bool`, `String`, `Vec3`, and `ActorID`.
  - Automatically folded into `World.Hash()` and exported by `World.Snapshot()` / `World.SnapshotJSON()`.
  - Directly readable and writable by declarative `BehaviorSpec` conditions, actions, Utility considerations, and GOAP planners.

```go
bb := self.Blackboard()
bb.SetFloat("energy", 85.0)
bb.AddFloat("captures", 1)
bb.SetActorID("target_id", target.ID)

energy := bb.FloatOr("energy", 0.0)
```

---

## 3. Choosing & Composing Decision Architectures

Every decision structure in `ntactor` implements `Behavior` (`Tick(dt, self, world)`), and `FSM`, `UtilitySelectorNode`, and `GOAPNode` also implement `BTNode` (`Evaluate(dt, self, world) NodeStatus`). This means you can use any architecture standalone **or** nest an `FSM`, `UtilitySelector`, or `GOAPNode` as a child inside a `BehaviorTree`.

| Architecture | Best For | Key Types |
| --- | --- | --- |
| **`FSM`** (`fsm.go`) | Explicit mode switching with enter/exit hooks and ordered transition guards | `NewFSM`, `FSMState`, `WildcardState` (`"*"`), `TimeInState()` |
| **`BehaviorTree`** (`bt.go`) | Hierarchical reactive priorities, sequences, cooldowns, and timeouts | `NewBehaviorTree`, `Selector`, `Sequence`, `MemSequence`, `Parallel`, `Cooldown`, `Timeout`, `Invert` |
| **`UtilitySelector`** (`utility.go`) | Continuous multi-factor trade-offs scored by normalized response curves | `NewUtilitySelector`, `UtilityOption`, `Consideration`, `ResponseCurve` (`CurveLinear`, `CurveQuadratic`, `CurveSqrt`, `CurveLogistic`, `CurveStep`) |
| **`GOAPNode`** (`goap.go`) | Multi-step STRIPS/F.E.A.R. action chaining over `Blackboard` preconditions & effects | `NewGOAP`, `PlanGOAP`, `GOAPGoal`, `GOAPAction`, `FactBool`, `FactFloat`, `FactInt`, `FactString` |

### A. Finite State Machine (`FSM`)

Transitions are checked in registration order before calling the active state's `OnTick`. Transitions from `WildcardState` (`"*"`) act as global interrupts (ignoring self-transitions when already in the `To` state):

```go
fsm := ntactor.NewFSM("patrol")
fsm.AddState("patrol", ntactor.FSMState{
    OnTick: patrolBehavior.Tick,
})
fsm.AddState("engage", ntactor.FSMState{
    OnEnter: func(self *ntactor.Actor, _ *ntactor.World) {
        self.Emit(ntactor.Intent{Kind: "alert"})
    },
    OnTick: pursueBehavior.Tick,
})
fsm.AddTransition(ntactor.WildcardState, "engage", func(self *ntactor.Actor, w *ntactor.World) bool {
    return w.NearestActor(self.Position, 12.0, self.ID) != nil
})
fsm.AddTransition("engage", "patrol", func(_ *ntactor.Actor, _ *ntactor.World) bool {
    return fsm.TimeInState() > 4.0
})
```

### B. Behavior Tree (`BehaviorTree`)

- **Stateless composites (`Sequence`, `Selector`)** re-evaluate from child `0` on every tick, allowing higher-priority branches in a `Selector` to immediately preempt a lower-priority `StatusRunning` action.
- **Stateful composites (`MemSequence`, `MemSelector`)** resume from the last `StatusRunning` child index until the sequence/selector completes.

```go
bt := ntactor.NewBehaviorTree(
    ntactor.Selector(
        ntactor.Sequence(
            ntactor.Condition(func(self *ntactor.Actor, w *ntactor.World) bool {
                return len(w.FindByTag("intruder")) > 0
            }),
            ntactor.Cooldown(1.5, engageAction),
        ),
        wanderAction,
    ),
)
```

### C. Utility AI (`UtilitySelector`)

Each `UtilityOption` combines one or more `Consideration` inputs normalized across `[Min, Max]` and shaped by a `ResponseCurve`. Multiple considerations on an option are combined via their geometric mean ($\left(\prod s_i\right)^{1/n} \times \text{Weight}$) so options with more considerations are not artificially penalized:

```go
u := ntactor.NewUtilitySelector(
    ntactor.UtilityOption{
        Name:   "intercept_intruder",
        Weight: 1.3,
        Considerations: []ntactor.Consideration{
            {
                Name: "intruder_proximity",
                Input: func(self *ntactor.Actor, w *ntactor.World) float64 {
                    if t := w.NearestActor(self.Position, 30.0, self.ID); t != nil {
                        return self.Position.Dist(t.Position)
                    }
                    return 30.0
                },
                Curve: ntactor.ResponseCurve{
                    Kind:   ntactor.CurveQuadratic,
                    Min:    0.0,
                    Max:    30.0,
                    Invert: true, // closer -> higher score
                },
            },
        },
        Node: pursueIntruderNode,
    },
    ntactor.UtilityOption{
        Name:   "patrol",
        Weight: 0.4,
        Node:   patrolNode,
    },
)
```

### D. Goal-Oriented Action Planning (`GOAPNode`)

`PlanGOAP` runs deterministic A\* over symbolic `Blackboard` facts to find the lowest-cost sequence of `GOAPAction` steps that satisfies a `GOAPGoal`. Unset keys default to closed-world zero values (`false`, `0`, `""`). `GOAPNode` executes the planned steps in order and automatically replans if preconditions fail or a higher-priority goal becomes unsatisfied:

```go
goap := ntactor.NewGOAP(
    []ntactor.GOAPGoal{
        {
            Name:     "neutralize_threat",
            Priority: 2.0,
            Desired:  []ntactor.GOAPFact{ntactor.FactBool("threat_cleared", true)},
        },
    },
    []ntactor.GOAPAction{
        {
            Name:          "arm_weapon",
            Cost:          1.0,
            Preconditions: []ntactor.GOAPFact{ntactor.FactBool("armed", false)},
            Effects:       []ntactor.GOAPFact{ntactor.FactBool("armed", true)},
            Node:          armWeaponNode,
        },
        {
            Name:          "engage_target",
            Cost:          2.0,
            Preconditions: []ntactor.GOAPFact{ntactor.FactBool("armed", true)},
            Effects:       []ntactor.GOAPFact{ntactor.FactBool("threat_cleared", true)},
            Node:          engageTargetNode,
        },
    },
    // Optional sensor callbacks run before each tick to sync World state into self.Blackboard():
    func(self *ntactor.Actor, w *ntactor.World) {
        hasThreat := len(w.FindByTag("intruder")) > 0
        self.Blackboard().SetBool("threat_cleared", !hasThreat)
    },
)
```

---

## 4. Declarative `BehaviorSpec`, Tracing, Rollouts & Tuning

`BehaviorRegistry` compiles validated JSON `BehaviorSpec` ASTs into executable `BehaviorTree` instances with zero `eval` or reflection.

### A. Registry & Schema Catalog

`NewStandardRegistry` registers built-in conditions, actions, and considerations wired to your optional `GridMap`, `FlowField`, `InfluenceMap`, and `Obstacles`:

```go
reg := ntactor.NewStandardRegistry(ntactor.StandardRegistryConfig{
    GridMap:      gm,
    FlowField:    ff,
    InfluenceMap: im,
    Obstacles:    obstacles,
})

// Export the JSON catalog of all node types and registered primitives (e.g. for an LLM prompt or UI):
catalogBytes, _ := reg.CatalogJSON()

// Compile a JSON BehaviorSpec into a BehaviorTree:
bt, err := reg.CompileJSON(specJSON)
```

You can register domain-specific conditions, actions, and considerations with parameter validation (`Min`, `Max`, `Required`, `Default`) using `reg.RegisterCondition`, `reg.RegisterAction`, and `reg.RegisterConsideration`.

### B. Supported `BehaviorSpec.Type` Values

- Composites: `"sequence"`, `"mem_sequence"`, `"selector"`, `"mem_selector"`, `"parallel"` (`required_successes`)
- Decorators: `"invert"`, `"cooldown"` (`seconds`), `"timeout"` (`seconds`)
- Leaves: `"condition"` (`name`, `params`), `"action"` (`name`, `params`)
- High-level architectures:
  - `"utility"` (`options`: `[]UtilityOptionSpec`)
  - `"fsm"` (`initial_state`, `states`: `[]FSMStateSpec`, `transitions`: `[]FSMTransitionSpec`)
  - `"goap"` (`sensors`: `[]GOAPSensorSpec`, `goals`: `[]GOAPGoalSpec`, `actions`: `[]GOAPActionSpec`)

### C. Live Execution Tracing (`TraceActor`)

Every compiled `BehaviorTree` records a per-tick `ExecutionTrace` showing the active node path, node statuses, `UtilitySelector` option scores, `FSM` active state, and `GOAP` goal + plan:

```go
if trace, ok := ntactor.TraceActor(unit); ok {
    fmt.Println("Active path:", trace.ActivePath)
    for _, entry := range trace.Entries {
        // Inspect entry.Path, entry.Status, entry.OptionScores, entry.FSMState, entry.GOAPGoal, entry.GOAPPlan
    }
}
```

### D. Counterfactual Rollouts & Evolutionary Tuning

Because `World` has no external side effects, you can clone or construct a fresh `World` in microseconds, simulate candidate specs for $N$ ticks, and rank or evolutionarily optimize them:

```go
// 1. Compare named candidate BehaviorSpecs across headless rollouts:
rankings, err := ntactor.CompareRollouts(baseRolloutCfg, candidates)
bestSpec := rankings[0].Spec

// 2. Evolutionarily perturb numeric parameters (speeds, radii, weights, curves, costs)
// within their registered [Min, Max] bounds using (1+lambda) hill-climbing:
opt, err := ntactor.OptimizeSpec(baseRolloutCfg, 8 /*generations*/, 6 /*offspring*/, 0.20 /*sigma*/)
tunedBT, _ := reg.Compile(opt.Best.Spec)
```

---

## 5. Steering, Formations & Collision Avoidance

All steering helpers in `steering.go` return a **desired velocity `Vec3`** (clamped to `maxSpeed`). Pass that desired velocity to `ApplySteering(self, desired, maxForce, maxSpeed, dt)` to apply acceleration clamping and Euler integration (`Integrate`).

| Helper | Purpose |
| --- | --- |
| `SteerSeek` / `SteerFlee` | Direct velocity toward or away from a target point |
| `SteerArrive` | Smoothly decelerates inside `slowRadius` |
| `SteerPursue` / `SteerEvade` | Lead-prediction intercept or evasion using target velocity |
| `SteerWander` | Deterministic angular jitter using `world.ActorRNG(self.ID)` |
| `SteerSeparation` / `SteerAlignment` / `SteerCohesion` | Classical Reynolds Boids neighborhood steering |
| `SteerAvoidCircles` | Forward feeler-box avoidance around static `CircleObstacle` pillars |
| `SteerAvoidActors` | Reciprocal time-to-closest-approach velocity avoidance for doorway/corridor traffic |
| `FormationOffsets` / `FollowLeaderBehavior` | `FormationWedge`, `FormationLine`, `FormationColumn`, and `FormationRing` slots rotated by leader heading |

```go
// Combine arrival with static obstacle avoidance and reciprocal Actor avoidance:
desired := ntactor.SteerArrive(self.Position, goal, maxSpeed, 3.0)
desired = desired.Add(ntactor.SteerAvoidCircles(self, obstacles, 2.5, maxSpeed))
desired = desired.Add(ntactor.SteerAvoidActors(self, world.Neighbors(self.ID, 3.0), 1.2, maxSpeed))
ntactor.ApplySteering(self, desired.ClampLen(maxSpeed), maxForce, maxSpeed, dt)
```

---

## 6. 2D/3D Navigation, Flow Fields & Spatial Influence Maps

### A. 2D `GridMap` & `FlowField`

- Use `GridMap.FindWorldPath(start, goal, smooth)` when individual Actors navigate to distinct destinations. Setting `smooth = true` runs supercover Bresenham line-of-sight string-pulling (`SmoothPath`) while preventing diagonal corner-clipping.
- Use `NewFlowField(gridMap)` + `flowField.Compute(goalX, goalY)` when many Actors share a common destination cell; `FlowFieldBehavior` steers each Actor along the bilinearly or cell-sampled Dijkstra vector field in $O(1)$ per Actor.

### B. 3D Navigation (`WaypointGraph` & `VoxelGrid`)

Both implement `NavGraph` and work directly with `FindPathNodes` and `FindPath`:

- **`WaypointGraph`** — arbitrary 3D points connected by directed or bidirectional weighted edges (`AddNode`, `AddBiEdge`, `NearestNode`, `FindWorldPath`), ideal for multi-floor ramps, jump pads, and portal networks.
- **`VoxelGrid`** — 3D volumetric occupancy and weighted-cost grid (`SetVoxelWall`, `SetBoxWall`, `SetVoxelCost`) with 26-connected 3D neighborhood search (preventing 3D diagonal corner clipping), 3D raycast `HasLineOfSight`, 3D `SmoothPath`, and `FindWorldPath`.

### C. Spatial Influence & Pheromone Maps (`InfluenceMap`)

`InfluenceMap` provides a deterministic 2D scalar field for tactical threat maps, scent/sound propagation, and stigmergic pheromone trails:

1. **Deposit / Stamp**:
   - `im.AddWorld(pos, delta)` — single-cell deposition.
   - `im.StampRadius(center, radius, peakDelta)` — linear radial falloff.
   - `im.StampRadiusWithLOS(center, radius, peakDelta, gridMap)` — radial falloff occluded by `GridMap` walls.
2. **Diffuse & Decay**:
   - `im.StepWithWalls(dt, decayRate, diffusionRate, gridMap)` — double-buffered 4-neighbor diffusion and decay that never leaks through wall cells.
3. **Steer Along Gradients**:
   - `SteerInfluence(self, im, maxSpeed, true)` climbs up pheromone/attraction gradients.
   - `SteerInfluence(self, im, maxSpeed, false)` flees down threat gradients.
4. **Couple Directly into Pathfinding**:
   - `im.ApplyCostToGridMap(gridMap, baseCost, weight)` maps cell influence into `GridMap` traversal costs ($\max(1, \text{baseCost} + \text{weight} \cdot \text{value})$) so subsequent `GridMap.FindWorldPath` (A\*) and `FlowField.Compute` calls automatically detour around high-threat danger zones (when `weight > 0`) or prefer high-pheromone highways (when `weight < 0` and `baseCost > 1`).

---

## 7. Live Recording & Bit-Exact Historical Replay

External inputs enter the simulation as `Event` values in `Frame` steps:

```go
// Live mode with recording enabled:
live := ntactor.NewLiveDriver(ntactor.LiveConfig{
    Seed:      42,
    DefaultDT: 1.0 / 60.0,
    Record:    true,
})
live.PushEvent(ntactor.Event{Kind: "spawn_beacon", Vector: ntactor.Vec2(10, 5)})
dt, events := live.Step()
world.Tick(dt, events)

// Serialize the recorded EventLog to compact binary or JSON:
blob, err := ntactor.MarshalEventLogBinary(live.Log())
log, err := ntactor.UnmarshalEventLogBinary(blob, 0)

// Replay on a fresh World with the same seed:
replayWorld := ntactor.NewWorld(ntactor.Config{Seed: log.Seed})
hist := ntactor.NewHistoricalDriver(log)
for !hist.Done() {
    dt, events := hist.Step()
    replayWorld.Tick(dt, events)
}
// replayWorld.Hash() == world.Hash()
```

### Mid-Simulation Checkpointing, Rollback & Branching (`checkpoint.go`)

In addition to `EventLog` replay from `tick=0`, you can snapshot, rewind, or fork a live `World` at **any tick**:

- **`cp := world.Checkpoint()` & `world.Restore(cp)`** — captures and restores `SimTime`, `TickCount`, `NextID`, exact `xoshiro256**` states for `world.RNG()` and every `world.ActorRNG(id)` stream, `world.Blackboard()`, and all active `Actor` kinematics, `Blackboard` entries, and stateful `Behavior` cursors (`*BehaviorTree`, `*FSM`, `*UtilitySelectorNode`, `*GOAPNode`, `*FollowPathBehavior`, `*PatrolBehavior`).
- **`branch := world.Clone()`** — returns an independent deep copy of `world` at the current tick so you can simulate future "what-if" trajectories without mutating the live `world`.
- **`RolloutConfig{BaseWorld: world, ...}`** — passing `BaseWorld` to `EvaluateRollout`, `CompareRollouts`, or `OptimizeSpec` forks directly from `world.Clone()` at the current live tick instead of starting from `tick=0`.
- **`EncodeCheckpointJSON` / `DecodeCheckpointJSON` + `world.RestoreWithRegistry(cp, reg)`** — serializes a `WorldCheckpoint` to JSON and restores it on a fresh `World`, recompiling each Actor's `"behavior_spec"` via `reg`.
