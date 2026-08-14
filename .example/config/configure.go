package config

import (
    "github.com/precision-soft/melody/.example/migration"
    melodyapplication "github.com/precision-soft/melody/application"
    bunormmigrate "github.com/precision-soft/melody/integrations/bunorm/migrate"
    melodycron "github.com/precision-soft/melody/integrations/cron"
)

func Configure(app *melodyapplication.Application) {
    app.RegisterModule(NewExampleModule())

    /* the cron Configuration reads app.cron.product_user off the kernel, so it travels as a factory evaluated at command-registration time; the runner commands are the same three constructors the Configuration schedules by name. WithDefaultParameters stays false because parameter.go registers the melody.cron.* parameters with example values. */
    app.RegisterModule(melodycron.NewModule(melodycron.ModuleConfig{
        ConfigurationFactory: newCronConfiguration,
        RunnerCommands:       cronRunnerCommands(),
    }))

    /* registered whether or not the database is configured, so the command surface does not change between environments — the same rule catalog:journal follows; without a database every db:* command fails at Run with the container refusal naming the registry service. */
    app.RegisterModule(bunormmigrate.NewModule(bunormmigrate.ModuleConfig{
        Migrations: migration.Migrations,
        Options: bunormmigrate.Options{
            ManagerRegistryServiceId: ServiceExampleDatabaseRegistry,
        },
    }))
}
