package main

import (
    "context"

    "github.com/precision-soft/melody/.example/config"
    "github.com/precision-soft/melody/application"
)

func main() {
    ctx := context.Background()

    app := application.NewApplication(
        embeddedEnvFiles,
        embeddedPublicFiles,
    )

    config.Configure(app)

    app.Run(ctx)
}
