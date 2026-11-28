package migrate

import (
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/uptrace/bun/migrate"
)

/* ContextConfig declares one migration context of a multi-database binary: its own migration set and its own command family (db:<name>:migrate, db:<name>:rollback, ...) pinned to one registry manager, so omitting the --manager flag can never migrate the wrong database. Zero-value ManagerRegistryServiceId, ManagerFlagName and CommandPrefix inherit from the base config handed to RegisterContextCommands, then from the defaults; UnlockTimeout inherits the same way; the command prefix derives as <basePrefix>:<name>. ManagerName is intentionally per-context: a context targets the manager named after it (its own name) unless it pins its own Command.ManagerName, and it never inherits the base pin, because a migration context must target its own database. */
type ContextConfig struct {
    Name       string
    Migrations *migrate.Migrations
    Command    CommandConfig
}

/* effectiveConfig resolves one context's command config: explicit context fields win, then the base config, then the defaults; CommandPrefix derives from the context name when neither the context nor the base pins it. ManagerName is resolved separately and does NOT inherit the base pin: an explicit per-context Command.ManagerName wins, otherwise it defaults to the context name, so a context always targets its own database rather than the base-pinned one. */
func effectiveConfig(contextConfig ContextConfig, baseConfig CommandConfig) CommandConfig {
    resolved := contextConfig.Command

    if "" == resolved.ManagerRegistryServiceId {
        resolved.ManagerRegistryServiceId = baseConfig.ManagerRegistryServiceId
    }
    if "" == resolved.ManagerRegistryServiceId {
        resolved.ManagerRegistryServiceId = DefaultManagerRegistryServiceId
    }

    if "" == resolved.ManagerFlagName {
        resolved.ManagerFlagName = baseConfig.ManagerFlagName
    }
    if "" == resolved.ManagerFlagName {
        resolved.ManagerFlagName = DefaultManagerFlagName
    }

    if "" == resolved.CommandPrefix {
        basePrefix := baseConfig.CommandPrefix
        if "" == basePrefix {
            basePrefix = DefaultCommandPrefix
        }

        resolved.CommandPrefix = basePrefix + ":" + contextConfig.Name
    }

    if true == resolved.UnlockTimeout.IsAbsent() {
        resolved.UnlockTimeout = baseConfig.UnlockTimeout
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
