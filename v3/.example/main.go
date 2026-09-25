package main

//go:generate go run . melody:routes:manifest --zone frontend --out assets/routes.json

import (
    "github.com/precision-soft/melody/v3/.example/config"
    "github.com/precision-soft/melody/v3/application"
    melodyexception "github.com/precision-soft/melody/v3/exception"
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

    config.Configure(ctx, app)

    /* the wiring is done, which is where the parallel teardown is armed: arming validates every declared teardown edge, so it needs the registrations, and walks the services the boot has already built. Arming asserts that every ordering these services need is written down, so the slowest closer does not hold the tracer provider; debug:container prints the plan, including the services nothing orders. */
    kernel := app.Boot()

    if armable, isArmable := kernel.ServiceContainer().(interface{ ArmParallelTeardown() error }); true == isArmable {
        if armErr := armable.ArmParallelTeardown(); nil != armErr {
            /* a panic raised here is outside Run, so no exit handler tears the booted container down on the way out: the logger's file, the pools and the broker connection the boot opened would go with the process unreleased. The application is closed first, and the refusal still ends the process the way a wiring mistake should. This close runs under no teardown budget and no shield — the configured budget is read by Run, which this path never reaches — so a closer that hangs here hangs the boot, which is the one place a wiring mistake is meant to be seen. */
            app.Close()

            melodyexception.Panic(melodyexception.FromError(armErr))
        }
    }

    app.Run()
}
