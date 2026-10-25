# ntactor roadmap

What is open, roughly in priority order within each section, followed by what
is already implemented.

## Next: Recordings

- [ ] **`ntrecord` demo tapes**: add deterministic `.tape` scripts to record
  `arena`, `flock`, `nav`, `scene3d`, and `planner` GIFs/PNGs for the README.

## Done

- [x] **Predictive Collision Avoidance, Squad Formations, and 3D Navigation (`steering.go`, `nav.go`, `spec.go`)**:
  reciprocal time-to-closest-approach avoidance (`SteerAvoidActors`), squad
  formations (`FormationKind`, `FormationOffsets`, `FormationWorldPosition`,
  `FollowLeaderBehavior`, `"follow_leader"` registry action), and 3D `NavGraph`
  implementations (`WaypointGraph` and `VoxelGrid` with 3D `HasLineOfSight`,
  `SmoothPath`, and `FindWorldPath`).

- [x] **Declarative `FSM` specs, `ExecutionTrace`, and Evolutionary Spec Tuning (`fsm.go`, `bt.go`, `utility.go`, `spec.go`, `examples/planner`)**:
  declarative `"fsm"` `BehaviorSpec` (`FSMStateSpec`, `FSMTransitionSpec` with
  `WildcardState` `"*"` and `after_seconds` timers), `FSM.AddNodeState` /
  `FSM.Evaluate`, per-tick `ExecutionTrace` (`TraceActor`,
  `BehaviorTree.LastTrace`, `UtilitySelectorNode.LastTrace`), and deterministic
  parameter perturbation + evolutionary tuning (`CloneSpec`, `MutateSpec`,
  `OptimizeSpec`).

- [x] **Declarative Behavior Synthesis, Utility AI, Blackboard & Rollouts (`blackboard.go`, `utility.go`, `spec.go`, `examples/planner`)**:
  deterministic sorted `Blackboard` on `World` and `Actor` (folded into
  `World.Hash()`), `World.SnapshotJSON()`, classical `UtilitySelector` +
  `ResponseCurve`, `BehaviorRegistry` (`NewStandardRegistry`, `CatalogJSON`,
  `CompileJSON`), `RegisterSwapHandler` for `Event`-driven behavior
  hot-swapping, and `EvaluateRollout` / `CompareRollouts`.
- [x] **`nt3d` live 3D scene demo (`examples/scene3d`)**: drive `nt3d` 3D
  meshes (`BuildCylinder`, `BuildSphere`, `BuildTorus`, `BuildHeightfield`),
  3D A\* path lines, and a 3D Boids murmuration point cloud from an
  `ntactor.World` in a Bubble Tea viewport with mouse camera orbit, ray-cast
  floor picking, and `HistoricalDriver` replay.
- [x] **Browser WASM gallery (`web/`, `wasm.work`, `go-booba`)**: compile
  `arena`, `flock`, `nav`, `scene3d`, and `planner` to `js/wasm` with
  `go-booba` and `ghostty-web`, OSC 7501 action buttons, Node smoke tests
  (`scripts/smoke-demo.mjs`), and GitHub Pages deployment
  (`.github/workflows/pages.yml`).
- [x] **Path planning & FlowField navigation (`nav.go`, `examples/nav`)**:
  `NavGraph`, `NodeID`, deterministic A\* (`FindPathNodes`, `FindPath`),
  2D occupancy/weighted `GridMap` with corner-clipping prevention, supercover
  `GridMap.HasLineOfSight`, string-pulling `SmoothPath`, `FindWorldPath`,
  `FollowPathBehavior`, `FlowField` (Dijkstra integration + vector field), and
  `FlowFieldBehavior`.
- [x] **Core Actor & World model (`actor.go`, `world.go`)**: `ActorID`, `Actor`,
  `Behavior`, `BehaviorFunc`, `IdleBehavior`, `Intent`, `Event`, deterministic
  ID-ordered `World.Tick`, `Spawn`/`Destroy` safe during ticks, and `World.Hash`
  FNV-1a state digest.
- [x] **Deterministic RNG (`rng.go`)**: seeded `xoshiro256**` PRNG via
  SplitMix64, pluggable `RandomSource`, and per-actor streams (`World.ActorRNG`).
- [x] **Spatial indexing & perception (`spatial.go`, `world.go`)**:
  `NewGridIndex` uniform 2D/3D spatial hash grid (`SpatialIndex`),
  `ActorsWithinRadius`, `Neighbors`, `NearestActor`, `FindByTag`,
  `ActorsWithinCone`, and pluggable `LineOfSight`.
- [x] **FSM & Behavior Trees (`fsm.go`, `bt.go`)**: `FSM` with `WildcardState`
  and `TimeInState`; `BehaviorTree` with `Sequence`, `MemSequence`, `Selector`,
  `MemSelector`, `Parallel`, `Condition`, `Action`, `Invert`, `Cooldown`, and
  `Timeout`.
- [x] **Steering & Boids Flocking (`steering.go`)**: `SteerSeek`, `SteerFlee`,
  `SteerArrive`, `SteerPursue`, `SteerEvade`, `SteerWander`, `SteerSeparation`,
  `SteerAlignment`, `SteerCohesion`, `SteerAvoidCircles`, `FlockBehavior`,
  `PatrolBehavior`, and `WanderBehavior`.
- [x] **Live & Historical Drivers + Replay Codecs (`driver_live.go`, `driver_historical.go`, `codec.go`)**:
  `LiveDriver`, `HistoricalDriver`, `Frame`, `EventLog`, `EncodeEventLogJSON` /
  `DecodeEventLogJSON`, and `MarshalEventLogBinary` / `UnmarshalEventLogBinary`.
- [x] **Examples (`examples/replay`, `examples/arena`, `examples/flock`, `examples/nav`, `examples/scene3d`, `examples/planner`)**:
  headless replay verifier, interactive terminal arena dashboard, Boids
  murmuration / point-cloud demo, interactive A\* + FlowField maze demo, 3D
  `nt3d` arena demo, and declarative behavior planner / rollout demo.
