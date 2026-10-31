# Changelog

## v0.2.0 (2026-10-30)

- **Checkpoints & rollouts**: Added `WorldCheckpoint`, `Restore`, and `Clone` for deterministic rollback and branching, JSON checkpoint encoding, and live `BaseWorld` rollouts.
- **Sensory memory**: Added Actor-owned `SensoryMemory`, visual perception, confidence decay, and `EmitSound`, with declarative memory primitives and planner demo integration.
- **Documentation**: Added `GUIDE.md` covering library usage, checkpoints, rollouts, and sensory memory.
- **Dependencies**: Updated example dependencies, including Bubble Tea v2.1.1, go-booba v0.10.7, nt3d v0.8.0, ntcanvas v0.1.3, ntpicture v0.1.4, and transitive dependencies. The root library remains dependency-free.

## v0.1.0 (2026-10-25)

Initial release of `ntactor`, a deterministic, renderer-agnostic Go library for game and simulation Actors.

- **Core simulation**: `World`, `Actor`, `Behavior`, `Intent`, `Event`, `Blackboard`, seeded `RNG` (`xoshiro256**`), 2D/3D `GridIndex` spatial queries, and `World.Hash()` state digests.
- **Decision making**: Finite State Machines (`FSM`), Behavior Trees (`BehaviorTree`), Utility AI (`UtilitySelector`, `ResponseCurve`), and Goal-Oriented Action Planning (`GOAPNode`, `PlanGOAP`).
- **Declarative behaviors**: JSON `BehaviorSpec` and `BehaviorRegistry` (`CompileJSON`, `CatalogJSON`, `TraceActor`, `RegisterSwapHandler`) with counterfactual rollout evaluation (`EvaluateRollout`, `CompareRollouts`) and evolutionary tuning (`OptimizeSpec`).
- **Steering & formations**: Classical steering (`SteerSeek`, `SteerFlee`, `SteerArrive`, `SteerPursue`, `SteerEvade`, `SteerWander`), Reynolds Boids (`FlockBehavior`), reciprocal collision avoidance (`SteerAvoidActors`), and squad formations (`FollowLeaderBehavior`, `FormationOffsets`).
- **Navigation & influence maps**: Deterministic A\* (`NavGraph`, `FindPath`), 2D `GridMap` with line-of-sight path smoothing (`SmoothPath`) and Dijkstra `FlowField`, 3D `WaypointGraph` and `VoxelGrid`, and 2D threat/pheromone `InfluenceMap` (`StampRadiusWithLOS`, `StepWithWalls`, `SteerInfluence`, `ApplyCostToGridMap`).
- **Replay & demos**: `LiveDriver`, `HistoricalDriver`, JSON/binary `EventLog` codecs, six examples (`arena`, `flock`, `nav`, `scene3d`, `planner`, `replay`), and a browser WASM demo gallery (`web/`).
