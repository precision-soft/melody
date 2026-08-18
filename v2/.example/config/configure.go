package config

import (
    bunormmigrate "github.com/precision-soft/melody/integrations/bunorm/migrate/v2"
    melodycron "github.com/precision-soft/melody/integrations/cron/v2"
    "github.com/precision-soft/melody/v2/.example/migration"
    melodyapplication "github.com/precision-soft/melody/v2/application"
)

func Configure(app *melodyapplication.Application) {
    app.RegisterModule(NewExampleModule())

    /* the cron Configuration reads app.cron.product_user off the kernel, so it travels as a factory evaluated at command-registration time; the runner commands are the same three constructors the Configuration schedules by name. WithDefaultParameters stays false because parameter.go registers the melody.cron.* parameters with example values. */
    app.RegisterModule(melodycron.NewModule(melodycron.ModuleConfig{
        ConfigurationFactory: newCronConfiguration,
        RunnerCommands:       cronRunnerCommands(),
    }))

    /* registered whether or not a database is configured, so the command surface does not change between environments — the same rule catalog:journal follows; without one every db:* command fails at Run with the container refusal naming the registry service. No context family is declared: this major keeps its journal on the same connection as the catalogue, so one set covers the whole schema and the registry has a single manager for the unprefixed commands to reach. */
    app.RegisterModule(bunormmigrate.NewModule(bunormmigrate.ModuleConfig{
        Migrations: migration.Migrations,
        Options: bunormmigrate.Options{
            ManagerRegistryServiceId: ServiceExampleDatabaseRegistry,
        },
    }))
}
