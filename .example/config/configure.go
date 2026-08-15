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

    /* registered whether or not a database is configured, so the command surface does not change between environments — the same rule catalog:journal follows; without one every db:* command fails at Run with the container refusal naming the registry service. The base family is pinned to the catalog manager by name, because with only the journal armed the registry's default falls back to the journal definition and an unpinned db:migrate would aim the mysql set at postgres; pinned, it refuses naming the absent manager instead. The journal context pins itself: its manager name defaults to the context name. */
    app.RegisterModule(bunormmigrate.NewModule(bunormmigrate.ModuleConfig{
        Migrations: migration.Migrations,
        Contexts: []bunormmigrate.ContextConfig{
            {
                Name:       databaseProviderNameJournal,
                Migrations: migration.JournalMigrations,
            },
        },
        Options: bunormmigrate.Options{
            ManagerRegistryServiceId: ServiceExampleDatabaseRegistry,
            ManagerName:              databaseProviderNameDefault,
        },
    }))
}
