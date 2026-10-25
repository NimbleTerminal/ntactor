# Declarative Behavior Planner demo: JSON BehaviorSpec, Utility AI, FSM, Rollouts, and OptimizeSpec.
# Run from the repository root: task tapes -- planner
Output dist/tapes/planner.gif
Source tapes/common.tape

Hide
Type@1ms "./bin/ntactor-planner"
Enter
Wait+Screen /ntactor Declarative Behavior Planner/
Sleep 400ms
Show
Sleep 1.2s

# Hot-swap to declarative FSM spec, spawn targets, and run evolutionary parameter tuning.
Type "s"
Sleep 1.2s
Type "b"
Sleep 800ms
Type "i"
Sleep 1s
Type "m"
Sleep 1.4s
Screenshot docs/images/planner.png
Sleep 1s

# Evaluate counterfactual rollouts and deploy top-ranked spec.
Type "e"
Sleep 1.5s
Hide
Type "q"
Wait+Screen /ntactor> /
