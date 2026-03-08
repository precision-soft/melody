package main

import (
    "github.com/precision-soft/melody/v3/.example/bootstrap"
    "github.com/precision-soft/melody/v3/application"
)

func main() {
    ctx, stop := application.NewSignalContext()
    defer stop()

    app := application.NewApplication(
        ctx,
        embeddedEnvFiles,
        embeddedPublicFiles,
    )

    bootstrap.Configure(app)

    app.Run()
}
