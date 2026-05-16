package config

import (
    melodyapplication "github.com/precision-soft/melody/application"
)

func Configure(app *melodyapplication.Application) {
    app.RegisterModule(NewExampleModule())
}
