package config

import (
    melodyapplication "github.com/precision-soft/melody/v2/application"
)

func Configure(app *melodyapplication.Application) {
    app.RegisterModule(NewExampleModule())
}
