# Changelog

## Unreleased

Initial implementation of `ntactor`, a deterministic, renderer-agnostic Go library for classical game and simulation actors.

- `Actor`, `ActorID`, `Behavior`, `BehaviorFunc`, `IdleBehavior`, `Intent`, and `Event`.
- `World` with a single simulation clock (`SimTime`), deterministic `ActorID` ordering, allocation-light `Tick`, lifecycle management (`Spawn`, `Destroy`, `Actor`, `ActorByID`, `Actors`), and radius perception (`ActorsWithinRadius`, `Neighbors`, `NearestActor`, `FindByTag`, pluggable `SpatialIndex`).
- Seeded deterministic `RNG` (`xoshiro256**` seeded via SplitMix64) and pluggable `RandomSource`, plus per-actor derived streams (`World.ActorRNG`).
- `FSM` (Finite State Machine with `OnEnter`, `OnTick`, `OnExit`, and ordered conditional transitions including `WildcardState`).
- Minimal `BehaviorTree` (`Sequence`, `Selector`, `Condition`, `Action`, `Invert`).
- Abstract `Vec3`/`Vec2` math and steering behaviors (`SteerSeek`, `SteerFlee`, `SteerArrive`, `SteerWander`, `SteerSeparation`, `ApplySteering`, `Integrate`, `PatrolBehavior`, `WanderBehavior`).
- `LiveDriver`, `HistoricalDriver`, `Frame`, and `EventLog` for recording live variable/fixed-step runs and replaying them bit-deterministically from seed.
- Standalone `examples` module with `examples/replay` and `examples/arena`.
