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
}
