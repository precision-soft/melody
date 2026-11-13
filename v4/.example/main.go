package main

//go:generate go run . melody:routes:manifest --zone frontend --out assets/routes.json

import (
    "github.com/precision-soft/melody/v4/.example/config"
    "github.com/precision-soft/melody/v4/application"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodylogging "github.com/precision-soft/melody/v4/logging"
)

/* applicationVersion is this application's own version, set by the build: go build -ldflags "-X main.applicationVersion=<version>". A build that sets nothing reports dev. */
var applicationVersion = "dev"

func main() {
    /* the signal context gives the application a graceful shutdown window on the first SIGINT or SIGTERM; a second signal during a hung shutdown forces the process down */
    ctx, stop := application.NewSignalContext()
    defer stop()

    /* Boot and Run recover their own panics; a refusal raised before Boot — the catalogue this example opens while it is wired — or after it, by the boot services below, reaches no such recovery. It is recorded here as an emergency, on the standard error because the journal does not exist yet or is closed by then, and the process exits 1 with the same final line a refusal inside Run leaves, instead of a bare goroutine dump and exit 2. */
    defer func() {
        melodylogging.LogOnRecoverAndExit(melodylogging.EmergencyLogger(), recover(), 1, melodylogging.ExitConfig{})
    }()

    app := application.NewApplication(
        ctx,
        application.ApplicationConfig{
            Version:          applicationVersion,
            EnvironmentFiles: embeddedEnvironmentFiles,
            PublicFiles:      embeddedPublicFiles,
        },
    )

    config.Configure(ctx, app, applicationVersion)

    /* the boot refuses a declared teardown edge that names no service, since the teardown closes in waves every service nothing orders; debug:container prints the plan, including the services nothing orders */
    kernel := app.Boot()

    /* the services built at boot rather than at first use; a panic raised here is outside Run, so no exit handler tears the booted container down on the way out, and the application is closed first */
    if resolveErr := config.ResolveBootServices(ctx, kernel.ServiceContainer()); nil != resolveErr {
        app.Close()

        melodyexception.Panic(melodyexception.FromError(resolveErr))
    }

    app.Run()
}
