package encrypt

import (
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/uptrace/bun"
)

func TestModule_RegisterCliCommandsBuildsFromDatabaseFactory(t *testing.T) {
    database := newMysqlDatabase()

    factoryCalled := false
    module := NewModule(ModuleConfig{
        Cipher: NewFakeCipher(),
        DatabaseFactory: func(resolver containercontract.Resolver) (*bun.DB, error) {
            factoryCalled = true

            return database, nil
        },
    })

    commands := module.RegisterCliCommands(nil)

    if false == factoryCalled {
        t.Fatalf("expected the database factory to be evaluated at RegisterCliCommands")
    }

    if 1 != len(commands) || "melody:encrypt:database" != commands[0].Name() {
        t.Fatalf("expected the melody:encrypt:database command from the factory, got %+v", commands)
    }
}

func TestModule_RegisterCliCommandsBuildsPerContextFromFactory(t *testing.T) {
    database := newMysqlDatabase()

    module := NewModule(ModuleConfig{
        Contexts: []CommandContextConfig{
            {
                Name:   "payment",
                Cipher: NewFakeCipher(),
                DatabaseFactory: func(resolver containercontract.Resolver) (*bun.DB, error) {
                    return database, nil
                },
            },
        },
    })

    commands := module.RegisterCliCommands(nil)

    if 1 != len(commands) || "melody:encrypt:database:payment" != commands[0].Name() {
        t.Fatalf("expected the per-context command from the factory, got %+v", commands)
    }
}

func TestModule_ResolveDatabasePrefersPrebuiltOverFactory(t *testing.T) {
    prebuilt := newMysqlDatabase()

    module := &Module{}

    resolved := module.resolveDatabase(
        prebuilt,
        func(resolver containercontract.Resolver) (*bun.DB, error) {
            t.Fatalf("factory must not run when a prebuilt database is present")

            return nil, nil
        },
        nil,
        "",
    )

    if prebuilt != resolved {
        t.Fatalf("expected the prebuilt database to win over the factory")
    }
}

func TestModule_RegisterCliCommandsPanicsWhenFactoryFails(t *testing.T) {
    module := NewModule(ModuleConfig{
        Cipher: NewFakeCipher(),
        DatabaseFactory: func(resolver containercontract.Resolver) (*bun.DB, error) {
            return nil, errFactory
        },
    })

    assertPanics(t, func() {
        module.RegisterCliCommands(nil)
    })
}

var errFactory = &factoryError{}

type factoryError struct{}

func (instance *factoryError) Error() string {
    return "factory failed"
}
