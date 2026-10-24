# ntactor roadmap

What is open, roughly in priority order within each section, followed by what
is already implemented.

## Next: Recordings & navigation follow-ups

- [ ] **`ntrecord` demo tapes**: add deterministic `.tape` scripts to record
  `arena`, `flock`, `nav`, and `scene3d` GIFs/PNGs for the README.
- [ ] **Dynamic local avoidance (ORCA / velocity obstacles)**: reciprocal
  collision avoidance for bi-directional doorway and narrow-corridor traffic.
- [ ] **3D navigation graphs (`NavMesh` / 3D voxel grids)**: multi-floor and 3D
  volumetric implementations of `NavGraph`.

## Done

- [x] **`nt3d` live 3D scene demo (`examples/scene3d`)**: drive `nt3d` 3D
  meshes (`BuildCylinder`, `BuildSphere`, `BuildTorus`, `BuildHeightfield`),
  3D A\* path lines, and a 3D Boids murmuration point cloud from an
  `ntactor.World` in a Bubble Tea viewport with mouse camera orbit, ray-cast
  floor picking, and `HistoricalDriver` replay.
- [x] **Browser WASM gallery (`web/`, `wasm.work`, `go-booba`)**: compile
  `arena`, `flock`, `nav`, and `scene3d` to `js/wasm` with `go-booba` and
  `ghostty-web`, OSC 7501 action buttons, Node smoke tests
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
  `LiveDriver`, `HistoricalDriver`, `EventLog`, `EncodeEventLogJSON` /
  `DecodeEventLogJSON`, and `MarshalEventLogBinary` / `UnmarshalEventLogBinary`.
- [x] **Examples (`examples/replay`, `examples/arena`, `examples/flock`, `examples/nav`)**:
  headless replay verifier, interactive terminal arena dashboard, Boids
  murmuration / point-cloud demo, and interactive A\* + FlowField maze demo.
