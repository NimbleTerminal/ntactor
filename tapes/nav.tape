# Navigation demo: Deterministic A* with LOS string-pulling and Dijkstra FlowField crowds.
# Run from the repository root: task tapes -- nav
Output dist/tapes/nav.gif
Source tapes/common.tape

Hide
Type@1ms "./bin/ntactor-nav"
Enter
Wait+Screen /ntactor Navigation/
Sleep 400ms
Show
Sleep 1.2s

# Move cursor, relocate goal, toggle flow arrows, and spawn another crowd wave.
Right@120ms 4
Down@120ms 2
Type "g"
Sleep 1.2s
Type "f"
Sleep 1s
Type "c"
Sleep 1.4s
Screenshot docs/images/nav.png
Sleep 1s
Hide
Type "q"
Wait+Screen /ntactor> /
