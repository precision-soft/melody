package main

import (
    "context"

    "github.com/precision-soft/melody/v2/.example/config"
    "github.com/precision-soft/melody/v2/application"
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
