# Boids Murmuration demo: Reynolds Separation, Alignment, Cohesion, GridIndex, and PointCloudAdapter.
# Run from the repository root: task tapes -- flock
Output dist/tapes/flock.gif
Source tapes/common.tape

Hide
Type@1ms "./bin/ntactor-flock"
Enter
Wait+Screen /ntactor Flock/
Sleep 400ms
Show
Sleep 1.2s

# Move the attractor and spawn a predator to scatter the flock.
Type "a"
Sleep 1.2s
Type "p"
Sleep 1.5s
Screenshot docs/images/flock.png
Sleep 1.2s

# Replay recorded murmuration from seed + EventLog.
Type "r"
Sleep 1.8s
Hide
Type "q"
Wait+Screen /ntactor> /
