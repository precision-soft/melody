package migrate

import (
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/uptrace/bun/migrate"
)

/* ContextConfig declares one migration context of a multi-database binary: its own migration set and its own command family (db:<name>:migrate, db:<name>:rollback, ...) pinned to one registry manager, so omitting the --manager flag can never migrate the wrong database. Zero-value ManagerRegistryServiceId, ManagerFlagName and CommandPrefix inherit from the base options handed to RegisterContextCommands, then from DefaultOptions(); the command prefix derives as <basePrefix>:<name>. ManagerName is intentionally per-context: a context targets the manager named after it (its own name) unless it pins its own Options.ManagerName, and it never inherits the base pin, because a migration context must target its own database. */
type ContextConfig struct {
    Name       string
    Migrations *migrate.Migrations
    Options    Options
}

/* effectiveOptions resolves one context's options: explicit context fields win, then the base options, then DefaultOptions(); CommandPrefix derives from the context name when neither the context nor the base pins it. ManagerName is resolved separately and does NOT inherit the base pin: an explicit per-context Options.ManagerName wins, otherwise it defaults to the context name, so a context always targets its own database rather than the base-pinned one. */
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
        /* ManagerName does NOT inherit baseOptions: a migration context must target its own database, so it defaults to the context name unless the context pins its own — inheriting a base pin here would silently route every context's commands at the base-pinned database */
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
