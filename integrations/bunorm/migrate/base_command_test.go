package migrate

import (
    "context"
    "errors"
    "reflect"
    "testing"

    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/integrations/bunorm"
    "github.com/precision-soft/melody/runtime"
    "github.com/uptrace/bun"
)

type stubResolver struct{}

func (instance *stubResolver) Get(serviceName string) (any, error) {
    return nil, errors.New("not implemented")
}

func (instance *stubResolver) MustGet(serviceName string) any {
    panic("not implemented")
}

func (instance *stubResolver) GetByType(targetType reflect.Type) (any, error) {
    return nil, errors.New("not implemented")
}

func (instance *stubResolver) MustGetByType(targetType reflect.Type) any {
    panic("not implemented")
}

func (instance *stubResolver) Has(serviceName string) bool {
    return false
}

func (instance *stubResolver) HasType(targetType reflect.Type) bool {
    return false
}

type stubProvider struct{}

func (instance *stubProvider) Open(resolver containercontract.Resolver) (*bun.DB, error) {
    return nil, errors.New("stub provider open must not be called for an unknown manager")
}

func TestResolveDatabase_UnknownManagerReturnsErrorInsteadOfPanic(t *testing.T) {
    registry, registryErr := bunorm.NewManagerRegistry(
        &stubResolver{},
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
