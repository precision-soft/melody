package main

//go:generate go run . melody:routes:manifest --zone frontend --out assets/routes.json

import (
    "github.com/precision-soft/melody/v3/.example/config"
    "github.com/precision-soft/melody/v3/application"
)

func main() {
    /* the signal context gives the application a graceful shutdown window on the first SIGINT or SIGTERM; a second signal during a hung shutdown forces the process down */
    ctx, stop := application.NewSignalContext()
    defer stop()

    app := application.NewApplication(
        ctx,
        embeddedEnvFiles,
        embeddedPublicFiles,
    )

    config.Configure(app)

    /* the wiring is done and nothing has been built yet, which is where the parallel teardown is armed: arming validates every declared teardown edge, so it needs the registrations, and it walks what a service holds from here on, so it wants to come before the first one is built.

       What this application asserts by arming it: every ordering its services need is written down. Measured on this wiring, the graph is thin — most of these services hold nothing of each other and none of them logs while closing — which is exactly why the teardown of the one that takes thirty seconds must not be what the tracer provider waits behind. `debug:container` prints the plan, including the services nothing orders. */
    kernel := app.Boot()

    if armable, isArmable := kernel.ServiceContainer().(interface{ ArmParallelTeardown() error }); true == isArmable {
        if armErr := armable.ArmParallelTeardown(); nil != armErr {
            panic(armErr)
        }
    }

    app.Run()
}
