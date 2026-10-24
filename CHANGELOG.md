# Changelog

## Unreleased

Initial implementation of `ntactor`, a deterministic, renderer-agnostic Go library for classical game and simulation actors.

- `Actor`, `ActorID`, `Behavior`, `BehaviorFunc`, `IdleBehavior`, `Intent`, and `Event`.
- `World` with a single simulation clock (`SimTime`), deterministic `ActorID` ordering, allocation-light `Tick`, `World.Hash()` FNV-1a state digest, lifecycle management (`Spawn`, `Destroy`, `Actor`, `ActorByID`, `Actors`), and perception (`ActorsWithinRadius`, `Neighbors`, `ActorsWithinCone`, `HasLineOfSight`, `NearestActor`, `FindByTag`, pluggable `SpatialIndex`).
- `GridIndex` (`NewGridIndex`) uniform 2D/3D spatial hash grid accelerating radius and cone perception while preserving deterministic `ActorID` ordering.
- Seeded deterministic `RNG` (`xoshiro256**` seeded via SplitMix64) and pluggable `RandomSource`, plus per-actor derived streams (`World.ActorRNG`).
- `FSM` (Finite State Machine with `OnEnter`, `OnTick`, `OnExit`, and ordered conditional transitions including `WildcardState`).
- `BehaviorTree` with stateless and stateful nodes (`Sequence`, `MemSequence`, `Selector`, `MemSelector`, `Parallel`, `Condition`, `Action`, `Invert`, `Cooldown`, `Timeout`).
- Abstract `Vec3`/`Vec2` math, classical steering, and Reynolds Boids flocking (`SteerSeek`, `SteerFlee`, `SteerArrive`, `SteerPursue`, `SteerEvade`, `SteerWander`, `SteerSeparation`, `SteerAlignment`, `SteerCohesion`, `SteerAvoidCircles`, `FlockBehavior`, `PatrolBehavior`, `WanderBehavior`).
- `LiveDriver`, `HistoricalDriver`, `Frame`, `EventLog`, and bounded JSON/binary `EventLog` codecs (`EncodeEventLogJSON`, `DecodeEventLogJSON`, `MarshalEventLogBinary`, `UnmarshalEventLogBinary`).
- Standalone `examples` module with `examples/replay`, `examples/arena`, and `examples/flock`.
