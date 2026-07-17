package main

import (
    "github.com/precision-soft/melody/.example/config"
    "github.com/precision-soft/melody/application"
)

func main() {
    /* the signal context gives the application a graceful shutdown window on the first SIGINT or SIGTERM; a second signal during a hung shutdown forces the process down */
    ctx, stop := application.NewSignalContext()
    defer stop()

    app := application.NewApplication(
        embeddedEnvFiles,
        embeddedPublicFiles,
    )

    config.Configure(app)

    app.Run(ctx)
}
