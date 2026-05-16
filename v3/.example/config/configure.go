package config

import (
    melodyapplication "github.com/precision-soft/melody/v3/application"
)

func Configure(app *melodyapplication.Application) {
    app.RegisterModule(NewExampleModule())
}
