package main

import (
    "context"

    "github.com/precision-soft/melody/v3/.example/config"
    "github.com/precision-soft/melody/v3/application"
)

func main() {
    ctx := context.Background()

    app := application.NewApplication(
        ctx,
        embeddedEnvFiles,
        embeddedPublicFiles,
    )

    config.Configure(app)

    app.Run()
}
