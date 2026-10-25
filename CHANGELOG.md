# Changelog

## Unreleased

Initial implementation of `ntactor`, a deterministic, renderer-agnostic Go library for classical game and simulation actors.

- `Actor`, `ActorID`, `Behavior`, `BehaviorFunc`, `IdleBehavior`, `Intent`, and `Event`.
- `World` with a single simulation clock (`SimTime`), deterministic `ActorID` ordering, allocation-light `Tick`, `World.Hash()` FNV-1a state digest, `World.Snapshot()` / `World.SnapshotJSON()`, lifecycle management (`Spawn`, `Destroy`, `Actor`, `ActorByID`, `Actors`), and perception (`ActorsWithinRadius`, `Neighbors`, `ActorsWithinCone`, `HasLineOfSight`, `NearestActor`, `FindByTag`, pluggable `SpatialIndex`).
- `Blackboard` (`Actor.Blackboard()`, `World.Blackboard()`) sorted, deterministic key-value memory store (`Float`, `Int`, `Bool`, `String`, `Vec3`, `ActorID`) folded into `World.Hash()`.
- `GridIndex` (`NewGridIndex`) uniform 2D/3D spatial hash grid accelerating radius and cone perception while preserving deterministic `ActorID` ordering.
- Seeded deterministic `RNG` (`xoshiro256**` seeded via SplitMix64) and pluggable `RandomSource`, plus per-actor derived streams (`World.ActorRNG`).
- `FSM` (Finite State Machine with `OnEnter`, `OnTick`, `OnExit`, `AddNodeState`, `Evaluate`, and ordered conditional transitions including `WildcardState`).
- `BehaviorTree` with stateless and stateful nodes (`Sequence`, `MemSequence`, `Selector`, `MemSelector`, `Parallel`, `Condition`, `Action`, `Invert`, `Cooldown`, `Timeout`) and per-tick `ExecutionTrace` (`LastTrace`, `TraceActor`).
- `UtilitySelector` (`UtilityOption`, `Consideration`, `ResponseCurve`, `ScoreConsiderations`, `LastTrace`) for classical Utility AI scoring with normalized response curves (`linear`, `quadratic`, `sqrt`, `logistic`, `step`).
- `BehaviorRegistry` & declarative `BehaviorSpec` (`NewStandardRegistry`, `CatalogJSON`, `Compile`, `CompileJSON`, `RegisterSwapHandler`, `EvaluateRollout`, `CompareRollouts`, `CloneSpec`, `MutateSpec`, `OptimizeSpec`) for compiling, hot-swapping, evaluating, and evolutionarily tuning JSON behavior trees, utility selectors, and finite state machines.
- Abstract `Vec3`/`Vec2` math, classical steering, predictive/reciprocal actor collision avoidance, squad formations, and Reynolds Boids flocking (`SteerSeek`, `SteerFlee`, `SteerArrive`, `SteerPursue`, `SteerEvade`, `SteerWander`, `SteerSeparation`, `SteerAlignment`, `SteerCohesion`, `SteerAvoidCircles`, `SteerAvoidActors`, `FormationKind`, `FormationOffsets`, `FormationWorldPosition`, `FollowLeaderBehavior`, `FlockBehavior`, `PatrolBehavior`, `WanderBehavior`).
- Deterministic 2D and 3D navigation and pathfinding (`NavGraph`, `NodeID`, `FindPathNodes`, `FindPath`, `GridMap` with weighted terrain and corner-clipping prevention, supercover `GridMap.HasLineOfSight`, string-pulling `SmoothPath`, `FindWorldPath`, `FollowPathBehavior`, `FlowField`, `FlowFieldBehavior`, 3D `WaypointGraph`, and 3D `VoxelGrid`).
- `LiveDriver`, `HistoricalDriver`, `Frame`, `EventLog`, and bounded JSON/binary `EventLog` codecs (`EncodeEventLogJSON`, `DecodeEventLogJSON`, `MarshalEventLogBinary`, `UnmarshalEventLogBinary`).
- Standalone `examples` module (`examples/replay`, `examples/arena`, `examples/flock`, `examples/nav`, `examples/scene3d`, `examples/planner`) and `booba` WASM browser demo gallery (`web/`).
