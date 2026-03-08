package main

import (
    "context"

    "github.com/precision-soft/melody/v3/.example/bootstrap"
    "github.com/precision-soft/melody/v3/application"
)

func main() {
    ctx := context.Background()

    app := application.NewApplication(
        embeddedEnvFiles,
        embeddedPublicFiles,
    )

    bootstrap.Configure(app)

    app.Run(ctx)
}
