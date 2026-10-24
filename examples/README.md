# Examples

Run these from the repository root with `task`:

| Command | What it shows | Controls / Flags |
| --- | --- | --- |
| `task run-replay` | Live simulation recording into an `EventLog`, historical replay verification, a glyph-grid adapter, and an abstract 3D scene-node adapter | `-seed 42`, `-steps 120`, `-json`, `-ascii` |
| `task run-arena` | Interactive terminal arena dashboard using `ntcanvas`, `GridIndex`, `FSM` guards, `BehaviorTree` scouts, and `FlockBehavior` wisps | `b`: beacon, `i`: intruder, `f`: flock wisps, `r`: replay history, `space`: pause, `n`: step, `x`: reset, `q`: quit (or `-headless -steps 60`) |
| `task run-flock` | Boids murmuration / particle-swarm demo (`FlockBehavior` + `GridIndex`) feeding both a contiguous `PointCloudAdapter` buffer and a terminal density grid | `a`: move attractor, `p`: spawn predator, `r`: replay history, `space`: pause, `n`: step, `q`: quit (or `-headless -count 120 -steps 40`) |
| `task run-nav` | Interactive A\* pathfinding (with LOS path smoothing), weighted terrain (`░`), and Dijkstra `FlowField` crowd navigation over a `GridMap` | `arrows/hjkl`: cursor, `w`: toggle wall, `g`: move goal, `s`: toggle smoothing, `c`: spawn crowd, `f`: flow arrows, `r`: replay, `q`: quit (or `-headless -steps 50`) |

The `examples` directory is a separate Go module (`nimbleterminal.dev/ntactor/examples`) so that terminal UI dependencies (`Bubble Tea`, `ntcanvas`) stay completely outside the root `nimbleterminal.dev/ntactor` module.
