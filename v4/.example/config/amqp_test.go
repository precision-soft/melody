package config

import (
    "context"
    "os"
    "path/filepath"
    "slices"
    "strings"
    "testing"

    amqp "github.com/precision-soft/melody/integrations/amqp/v4"
    melodycontainer "github.com/precision-soft/melody/v4/container"
)

func TestAmqpModuleConfig_TheConnectionIsBuiltAtBootAndWarns(t *testing.T) {
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })

    moduleInstance := moduleWithEnvironment(t, map[string]string{environmentKeyAmqpDsn: "amqp://guest:guest@broker.invalid:5672/"})
    moduleInstance.amqpModule().RegisterServices(containerRegistrar{Container: containerInstance})

    names, modes := builtAtBootNames(containerInstance)
    if false == slices.Equal([]string{amqp.ServiceConnection}, names) || melodycontainer.BootFailureWarns != modes[amqp.ServiceConnection] {
        t.Fatalf("expected the amqp connection alone built at boot with the warning mode, got %v %v", names, modes)
    }

    if true == containerInstance.Has(serviceOutboxTransport) || true == containerInstance.Has(messageBusTransportAsync) {
        t.Fatalf("expected the module to register no transport")
    }
}

func TestAmqpModule_IsRegisteredOnlyBesideAmqpDsn(t *testing.T) {
    if nil != moduleWithEnvironment(t, map[string]string{}).amqpModule() {
        t.Fatal("expected no amqp module without AMQP_DSN")
    }

    if nil == moduleWithEnvironment(t, map[string]string{environmentKeyAmqpDsn: "amqp://guest:guest@rabbitmq:5672/"}).amqpModule() {
        t.Fatal("expected the amqp module beside AMQP_DSN")
    }
}

func TestAmqpProvider_IsTheOneProviderOfTheExample(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{})

    first := moduleInstance.amqpProvider()
    if nil == first || first != moduleInstance.amqpProvider() {
        t.Fatalf("expected one provider answered twice, got %p and %p", first, moduleInstance.amqpProvider())
    }

    sourcePaths, globErr := filepath.Glob("*.go")
    if nil != globErr {
        t.Fatalf("glob: %v", globErr)
    }

    for _, sourcePath := range sourcePaths {
        if true == strings.HasSuffix(sourcePath, "_test.go") || "amqp.go" == sourcePath {
            continue
        }

        source, readErr := os.ReadFile(sourcePath)
        if nil != readErr {
            t.Fatalf("read %s: %v", sourcePath, readErr)
        }

        if true == strings.Contains(string(source), "amqp.NewProvider(") {
            t.Fatalf("%s: expected the transports to dial through amqpProvider, not a provider of their own", sourcePath)
        }
    }
}
