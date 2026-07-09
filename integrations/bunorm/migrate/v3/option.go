package migrate

func DefaultOptions() Options {
    return Options{
        ManagerRegistryServiceId: "service.database.manager.registry",
        ManagerFlagName:          "manager",
        CommandPrefix:            "db",
    }
}

type Options struct {
    ManagerRegistryServiceId string
    ManagerFlagName          string
    CommandPrefix            string

    /* ManagerName pins the registry manager these commands use when the --manager flag is absent; empty falls back to the registry default. Pin one manager per command set so a multi-context binary can never migrate the wrong database by omitting the flag. */
    ManagerName string
}
