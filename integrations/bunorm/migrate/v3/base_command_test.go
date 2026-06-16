package migrate

import (
    "context"
    "errors"
    "testing"

    "github.com/precision-soft/melody/integrations/bunorm/v3"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    "github.com/uptrace/bun"
)

type stubProvider struct{}

func (instance *stubProvider) Open(params bunorm.ConnectionParams, logger loggingcontract.Logger) (*bun.DB, error) {
    return nil, errors.New("stub provider open must not be called for an unknown manager")
}

func TestResolveDatabase_UnknownManagerReturnsErrorInsteadOfPanic(t *testing.T) {
    registry, registryErr := bunorm.NewManagerRegistry(
        logging.NewNopLogger(),
        bunorm.ProviderDefinition{Name: "primary", Provider: &stubProvider{}, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("failed to build manager registry: %s", registryErr.Error())
    }

    options := DefaultOptions()

    serviceContainer := container.NewContainer()
    container.MustRegister[*bunorm.ManagerRegistry](
        serviceContainer,
        options.ManagerRegistryServiceId,
        func(resolver containercontract.Resolver) (*bunorm.ManagerRegistry, error) {
            return registry, nil
        },
    )

    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
    base := &baseCommand{options: options}

    var resolveErr error
    didPanic := false

    command := &clicontract.CommandContext{
        Name:  "migrate",
        Flags: []clicontract.Flag{&clicontract.StringFlag{Name: options.ManagerFlagName}},
        Action: func(ctx context.Context, commandContext *clicontract.CommandContext) error {
            defer func() {
                if recovered := recover(); nil != recovered {
                    didPanic = true
                }
            }()

            _, _, resolveErr = base.resolveDatabase(runtimeInstance, commandContext)

            return nil
        },
    }

    _ = command.Run(context.Background(), []string{"migrate", "--" + options.ManagerFlagName, "unknown"})

    if true == didPanic {
        t.Fatalf("resolveDatabase panicked on an unknown manager name instead of returning an error")
    }

    if nil == resolveErr {
        t.Fatalf("expected an error for an unknown manager name, got nil")
    }
}
