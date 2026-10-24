# Examples

Run these from the repository root with `task`. Each interactive browser demo publishes its key bindings through [`booba`](https://github.com/NimbleMarkets/go-booba) so the same actions appear as interactive buttons above the terminal.

| Command | What it shows | Controls / Flags |
| --- | --- | --- |
| `task run-arena` | Interactive terminal arena dashboard using `ntcanvas`, `GridIndex`, `FSM` guards (`SteerPursue`), `BehaviorTree` scouts, and `FlockBehavior` wisps | `b`: beacon, `i`: intruder, `f`: flock wisps, `r`: replay history, `space`: pause, `n`: step, `x`: reset, `q`: quit (or `-- -headless -steps 60`) |
| `task run-flock` | Reynolds Boids murmuration (`FlockBehavior` + `GridIndex`) feeding both a contiguous 3D `PointCloudAdapter` buffer and a terminal density grid | `a`: move attractor, `p`: spawn predator, `r`: replay history, `space`: pause, `n`: step, `x`: reset, `q`: quit (or `-- -headless -count 120 -steps 40`) |
| `task run-nav` | Interactive A\* pathfinding (with supercover LOS path smoothing), weighted terrain (`░`), and Dijkstra `FlowField` crowd navigation over a `GridMap` | `arrows/hjkl`: cursor, `w`: toggle wall, `g`: move goal, `s`: toggle smoothing, `c`: spawn crowd, `f`: flow arrows, `r`: replay, `q`: quit (or `-- -headless -steps 50`) |
| `task run-replay` | CLI live simulation recording into an `EventLog`, JSON/binary serialization, historical replay verification, a glyph-grid adapter, and an abstract 3D scene-node adapter | `-seed 42`, `-steps 120`, `-json`, `-ascii` |

## Browser / WASM Demo Gallery

`task serve-wasm-site` builds all three interactive demos (`arena`, `flock`, `nav`) to `web/demos/*.wasm`, stages the `booba` and `ghostty-web` runtime assets into `web/`, smoke-tests each binary under Node, and serves the gallery at `http://localhost:8000/web/`:

```sh
task serve-wasm-site
```

Switch demos via the top navigation cards or query parameters (`?demo=arena`, `?demo=flock`, `?demo=nav`).

The `examples` directory is a separate Go module (`nimbleterminal.dev/ntactor/examples`) so that terminal and browser UI dependencies (`Bubble Tea`, `ntcanvas`, `go-booba`) stay completely outside the root `nimbleterminal.dev/ntactor` module.
