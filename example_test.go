// Copyright (c) 2026 Neomantra Corp.

package ntactor_test

import (
	"fmt"

	actor "nimbleterminal.dev/ntactor"
)

type PatrolState struct {
	Speed float64
}

type myPatrolBehavior struct{}

func (myPatrolBehavior) Tick(dt float64, self *actor.Actor, _ *actor.World) {
	st := self.State.(*PatrolState)
	self.Velocity = actor.Vec2(st.Speed, 0)
	actor.Integrate(self, dt)
}

func ExampleNewWorld() {
	world := actor.NewWorld(actor.Config{Seed: 42})

	id := world.Spawn(actor.Actor{
		Behavior: myPatrolBehavior{},
		State:    &PatrolState{Speed: 2.5},
	})

	driver := actor.NewHistoricalDriverFromFrames(
		actor.Frame{DT: 0.5},
		actor.Frame{DT: 0.5},
	)

	for !driver.Done() {
		dt, events := driver.Step()
		world.Tick(dt, events)
	}

	a := world.ActorByID(id)
	fmt.Printf("id=%d pos=(%.1f, %.1f) simTime=%.1f\n", a.ID, a.Position.X, a.Position.Y, world.SimTime())
	// Output:
	// id=1 pos=(2.5, 0.0) simTime=1.0
}
