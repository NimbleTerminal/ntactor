# Examples

Run these from the repository root with `task`:

| Command | What it shows | Controls / Flags |
| --- | --- | --- |
| `task run-replay` | Live simulation recording into an `EventLog`, historical replay verification, a glyph-grid adapter, and an abstract 3D scene-node adapter | `-seed 42`, `-steps 120`, `-json`, `-ascii` |
| `task run-arena` | Interactive terminal arena using `ntcanvas` and Bubble Tea over an abstract `ntactor.World` | `b`: spawn beacon, `i`: spawn intruder, `r`: replay recorded history, `space`: pause, `n`: step, `q`: quit (or `-headless -steps 60`) |

The `examples` directory is a separate Go module (`nimbleterminal.dev/ntactor/examples`) so that terminal UI dependencies (`Bubble Tea`, `ntcanvas`) stay completely outside the root `nimbleterminal.dev/ntactor` module.
