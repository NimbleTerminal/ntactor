# 3D Arena demo (ntactor x nt3d): 3D meshes, A* paths, and 3D Boids murmuration point cloud.
# Run from the repository root: task tapes -- scene3d
Output dist/tapes/scene3d.gif
Source tapes/common.tape

Hide
Type@1ms "./bin/ntactor-scene3d"
Enter
Wait+Screen /Scene3D \[LIVE\]/
Sleep 1.2s
Show
Sleep 1s

# Orbit camera, spawn 3D beacons and an intruder so A* path lines and alert cones appear.
Type "l"
Sleep 400ms
Type "b"
Sleep 200ms
Type "b"
Sleep 200ms
Type "i"
Sleep 350ms
Screenshot docs/images/scene3d.png
Sleep 2s
Hide
Type "q"
Wait+Screen /ntactor> /
