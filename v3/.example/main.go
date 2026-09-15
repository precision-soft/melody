package main

//go:generate go run . melody:routes:manifest --zone frontend --out assets/routes.json

import (
    "github.com/precision-soft/melody/v3/.example/config"
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

    config.Configure(ctx, app)

    kernel := app.Boot()

    if armable, isArmable := kernel.ServiceContainer().(interface{ ArmParallelTeardown() error }); true == isArmable {
        if armErr := armable.ArmParallelTeardown(); nil != armErr {

            app.Close()

            panic(armErr)
        }
    }

    app.Run()
}
