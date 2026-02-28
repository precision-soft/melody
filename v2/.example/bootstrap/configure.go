package bootstrap

import (
    melodyapplication "github.com/precision-soft/melody/v2/application"
)

func Configure(app *melodyapplication.Application) {
    registerServices(app)

    app.RegisterModule(NewExampleModule())

    app.RegisterHttpMiddlewares(
        NewTimingMiddleware(),
    )
}
