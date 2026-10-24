# ntactor roadmap

What is open, roughly in priority order within each section, followed by what
is already implemented.

## Next: Path planning & navigation

- [ ] **Abstract graph & grid A\* pathfinding (`nav.go`)**: deterministic A\*
  over a caller-supplied `NavGraph` or 2D/3D occupancy grid (`Cost(from, to)`,
  `Neighbors(node)`), returning waypoint paths (`[]Vec3`) that feed directly
  into `PatrolBehavior` or a `FollowPathBehavior`.
- [ ] **Jump Point Search (JPS) & path smoothing**: uniform-cost grid
  acceleration and string-pulling / line-of-sight funnel smoothing using
  `World.HasLineOfSight`.
- [ ] **Flow fields (`FlowField`)**: compute a deterministic vector field toward
  a goal cell once per target change so hundreds or thousands of swarm/crowd
  Actors steer around walls in $O(1)$ per Actor per tick.
- [ ] **Dynamic local avoidance (ORCA / velocity obstacles)**: reciprocal
  collision avoidance for dense doorway and corridor navigation.

## Sister-library demos & web gallery

- [ ] **`nt3d` live 3D scene demo (`examples/scene3d`)**: drive `nt3d` instanced
  meshes, `nt3d/aabbtree`, and `nt3d` point clouds from an `ntactor.World` in a
  Bubble Tea terminal viewport with camera orbit.
- [ ] **Browser WASM gallery (`web/` + `go-booba`)**: compile `arena` and
  `flock` to `js/wasm` with `go-booba` and `ghostty-web` for GitHub Pages.
- [ ] **`ntrecord` demo tapes**: add deterministic `.tape` scripts to record
  `arena` and `flock` GIFs/PNGs for the README.

## Done

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
- [x] **Examples (`examples/replay`, `examples/arena`, `examples/flock`)**:
  headless replay verifier, interactive terminal arena dashboard, and Boids
  murmuration / point-cloud demo.
