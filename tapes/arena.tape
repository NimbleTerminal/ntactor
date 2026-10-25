# Tactical Arena demo: FSM guards, BT scouts, Boid wisps, and deterministic replay.
# Run from the repository root: task tapes -- arena
Output dist/tapes/arena.gif
Source tapes/common.tape

Hide
Type@1ms "./bin/ntactor-arena"
Enter
Wait+Screen /ntactor Arena/
Sleep 400ms
Show
Sleep 1.2s

# Spawn a beacon, intruder, and flock wisps.
Type "b"
Sleep 1s
Type "i"
Sleep 1.2s
Type "f"
Sleep 1.5s
Screenshot docs/images/arena.png
Sleep 1s

# Trigger deterministic HistoricalDriver replay from seed + EventLog.
Type "r"
Sleep 1.8s
Hide
Type "q"
Wait+Screen /ntactor> /
