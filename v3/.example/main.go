package main

//go:generate go run . melody:routes:manifest --zone frontend --out assets/routes.json

import (
    "github.com/precision-soft/melody/v3/.example/config"
    "github.com/precision-soft/melody/v3/application"
    melodyclioutput "github.com/precision-soft/melody/v3/cli/output"
    melodyexception "github.com/precision-soft/melody/v3/exception"
    melodylogging "github.com/precision-soft/melody/v3/logging"
)

/* applicationVersion is this application's own version, set by the build: go build -ldflags "-X main.applicationVersion=<version>". A build that sets nothing reports dev. */
var applicationVersion = "dev"

func main() {
    /* the signal context gives the application a graceful shutdown window on the first SIGINT or SIGTERM; a second signal during a hung shutdown forces the process down */
    ctx, stop := application.NewSignalContext()
    defer stop()

    /* Boot and Run recover their own panics; a refusal raised before Boot — the catalogue this example opens while it is wired — or after it, by the boot services and the arming below, reaches no such recovery. It is recorded here as an emergency, on the standard error because the journal does not exist yet or is closed by then, and the process exits 1 with the same final line a refusal inside Run leaves, instead of a bare goroutine dump and exit 2. */
    defer func() {
        melodylogging.LogOnRecoverAndExit(melodylogging.EmergencyLogger(), recover(), 1)
    }()

    /* every command document names this version in its meta, and debug:version prints it beside melody's */
    melodyclioutput.SetApplicationVersion(applicationVersion)

    app := application.NewApplication(
        ctx,
        embeddedEnvFiles,
        embeddedPublicFiles,
    )

    config.Configure(ctx, app, applicationVersion)

    /* the wiring is done, which is where the parallel teardown is armed: arming validates every declared teardown edge, so it needs the registrations, and walks the services the boot has already built. Arming asserts that every ordering these services need is written down, so the slowest closer does not hold the tracer provider; debug:container prints the plan, including the services nothing orders. */
    kernel := app.Boot()

    /* the services built at boot rather than at first use, before the arming so it walks them too; a refusal closes the application first, as the arming's does */
    if resolveErr := config.ResolveBootServices(kernel.ServiceContainer()); nil != resolveErr {
        app.Close()

        melodyexception.Panic(melodyexception.FromError(resolveErr))
    }

    if armable, isArmable := kernel.ServiceContainer().(interface{ ArmParallelTeardown() error }); true == isArmable {
        if armErr := armable.ArmParallelTeardown(); nil != armErr {
            /* a panic raised here is outside Run, so no exit handler tears the booted container down on the way out: the logger's file, the pools and the broker connection the boot opened would go with the process unreleased. The application is closed first, and the refusal still ends the process the way a wiring mistake should. This close runs under no teardown budget and no shield — the configured budget is read by Run, which this path never reaches — so a closer that hangs here hangs the boot, which is the one place a wiring mistake is meant to be seen. */
            app.Close()

            melodyexception.Panic(melodyexception.FromError(armErr))
        }
    }

    app.Run()
}
