// Copyright (c) 2026 Neomantra Corp.

// Package ntactor provides deterministic, abstract game and simulation actors
// and their behaviors.
//
// Actors and Worlds own only identity, local state, abstract coordinates, and
// behavior. They know nothing about meshes, scene-graph nodes, transforms,
// terminal glyphs, pixels, or rendering. Visual or physical presentation is
// handled by external adapters that read actor state or emitted intents after
// each simulation tick.
//
// Simulation time advances exclusively through deterministic delta steps passed
// to World.Tick, and all pseudo-randomness flows through a World-owned seeded
// RNG. Given the same seed and the same sequence of (dt, events) frames, a
// simulation run is bit-deterministic across live and historical replay modes.
package ntactor
