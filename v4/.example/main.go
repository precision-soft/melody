package main

//go:generate go run . melody:routes:manifest --zone frontend --out assets/routes.json

import (
    "github.com/precision-soft/melody/v4/.example/config"
    "github.com/precision-soft/melody/v4/application"
    melodylogging "github.com/precision-soft/melody/v4/logging"
)

/* applicationVersion is this application's own version, set by the build: go build -ldflags "-X main.applicationVersion=<version>". A build that sets nothing reports dev. */
var applicationVersion = "dev"

func main() {
    /* the signal context gives the application a graceful shutdown window on the first SIGINT or SIGTERM; a second signal during a hung shutdown forces the process down */
    ctx, stop := application.NewSignalContext()
    defer stop()

    /* Boot and Run recover their own panics; a refusal raised before Boot — the catalogue this example opens while it is wired — reaches no such recovery. It is recorded here as an emergency, on the standard error because the journal does not exist yet, and the process exits 1 with the same final line a refusal inside Run leaves, instead of a bare goroutine dump and exit 2. */
    defer func() {
        melodylogging.LogOnRecoverAndExit(melodylogging.EmergencyLogger(), recover(), 1, melodylogging.ExitConfig{})
    }()

    app := application.NewApplication(
        ctx,
        application.ApplicationConfig{
            Version:            applicationVersion,
            EnvironmentFiles:   embeddedEnvironmentFiles,
            PublicFiles:        embeddedPublicFiles,
            EnvironmentKeys:    config.EnvironmentKeys(),
            ParameterOverrides: config.ParameterOverrides(),
        },
    )

    config.Configure(ctx, app, applicationVersion)

    /* the boot refuses a declared teardown edge that names no service, since the teardown closes in waves every service nothing orders; debug:container prints the plan, including the services nothing orders */
    app.Boot()

    app.Run()
}
