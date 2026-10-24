# Working on ntactor

## Overview

`ntactor` (`nimbleterminal.dev/ntactor`) is a self-contained, pure-Go library providing classical game and simulation Actors and their Behaviors.

## Core Constraints

- **Terminology**: Always use the noun **Actor**. Never use "agent" in library types, functions, or documentation.
- **Abstraction boundary**: Root `nimbleterminal.dev/ntactor` has zero external dependencies (standard library only). Actors and Worlds know nothing about meshes, scene-graph nodes, transforms, terminal glyphs, pixels, or rendering. Adapters in external packages read `world.Actors()` and `world.Intents()` after `world.Tick`.
- **Deterministic time**: Simulation time advances only through `dt` passed to `World.Tick(dt, events)`. Never import `"time"` or call wall-clock APIs (`time.Now`, etc.) in library source files or behaviors.
- **Deterministic randomness**: All randomness flows through `World.RNG()` or `World.ActorRNG(id)`. Never use global `math/rand` or `crypto/rand`.
- **Deterministic iteration**: Never iterate over Go maps during `World.Tick` or query returns; preserve deterministic `ActorID` order in dense slices.
- **Examples isolation**: Terminal and UI dependencies (`Bubble Tea`, `ntcanvas`) live strictly inside `examples/go.mod`.

## Verification

Run `task ci` before finishing changes to verify formatting, module tidiness, race-detector tests, `go vet`, cross-platform builds (`windows`, `linux`, `darwin`, `freebsd`, `js/wasm`), and example binaries.
