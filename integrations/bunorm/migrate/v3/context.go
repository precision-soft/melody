package migrate

import (
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/uptrace/bun/migrate"
)

/* ContextConfig declares one migration context of a multi-database binary: its own migration set and its own command family (db:<name>:migrate, db:<name>:rollback, ...) pinned to one registry manager, so omitting the --manager flag can never migrate the wrong database. Zero-value Options fields inherit from the base options handed to RegisterContextCommands, then from DefaultOptions(); by convention the pinned manager name defaults to the context name and the command prefix to <basePrefix>:<name>. */
type ContextConfig struct {
    Name       string
    Migrations *migrate.Migrations
    Options    Options
}

/* effectiveOptions resolves one context's options: explicit context fields win, then the base options, then DefaultOptions(); CommandPrefix and ManagerName derive from the context name when neither the context nor the base pins them. */
func effectiveOptions(contextConfig ContextConfig, baseOptions Options) Options {
    defaults := DefaultOptions()

    resolved := contextConfig.Options

    if "" == resolved.ManagerRegistryServiceId {
        resolved.ManagerRegistryServiceId = baseOptions.ManagerRegistryServiceId
    }
    if "" == resolved.ManagerRegistryServiceId {
        resolved.ManagerRegistryServiceId = defaults.ManagerRegistryServiceId
    }

    if "" == resolved.ManagerFlagName {
        resolved.ManagerFlagName = baseOptions.ManagerFlagName
    }
    if "" == resolved.ManagerFlagName {
        resolved.ManagerFlagName = defaults.ManagerFlagName
    }

    if "" == resolved.CommandPrefix {
        basePrefix := baseOptions.CommandPrefix
        if "" == basePrefix {
            basePrefix = defaults.CommandPrefix
        }

        resolved.CommandPrefix = basePrefix + ":" + contextConfig.Name
    }

    if "" == resolved.ManagerName {
        resolved.ManagerName = contextConfig.Name
    }

    return resolved
}

func validateContexts(contexts []ContextConfig) {
    seen := make(map[string]bool, len(contexts))

    for _, contextConfig := range contexts {
        if "" == contextConfig.Name {
            exception.Panic(exception.NewError("migration context name is empty", nil, nil))
        }

        if nil == contextConfig.Migrations {
            exception.Panic(
                exception.NewError(
                    "migration context migrations are nil",
                    exceptioncontract.Context{
                        "context": contextConfig.Name,
                    },
                    nil,
                ),
            )
        }

        if true == seen[contextConfig.Name] {
            exception.Panic(
                exception.NewError(
                    "duplicate migration context name",
                    exceptioncontract.Context{
                        "context": contextConfig.Name,
                    },
                    nil,
                ),
            )
        }

        seen[contextConfig.Name] = true
    }
}
