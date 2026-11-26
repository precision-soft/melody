package config

import (
    "bytes"
    "errors"
    "os"
    "path/filepath"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/.example/subscriber"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    melodysecurity "github.com/precision-soft/melody/v4/security"
)

func TestBuiltAtBoot_HandsTheHubItsJournalBeforeAnyWrite(t *testing.T) {
    journal := &bytes.Buffer{}

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    containerInstance.MustRegister(
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewJsonLogger(journal, melodylogging.JsonLoggerConfig{MinLevel: melodyloggingcontract.LevelDebug}), nil
        },
        melodycontainer.RegisterOptions{},
    )

    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.buildInternalAuth()
    moduleInstance.registerServerSentEventHubService(containerRegistrar{Container: containerInstance})
    moduleInstance.registerInternalAuthSignerService(containerRegistrar{Container: containerInstance})

    if resolveErr := buildServicesBuiltAtBoot(t, containerInstance); nil != resolveErr {
        t.Fatalf("unexpected boot resolution error: %v", resolveErr)
    }

    hub := moduleInstance.builtServerSentEventHub.Load()
    hub.SetBackplane(refusingBackplane{})
    hub.Broadcast("catalog", melodyhttp.ServerSentEvent{Event: "notification", Data: "changed"})
    _ = hub.CloseWithContext(t.Context())

    if false == strings.Contains(journal.String(), "server sent event backplane publish failed") {
        t.Fatalf("expected the refused publish in the container's journal, got %q", journal.String())
    }
}

func bootContainerWithInternalAuthSigner(t *testing.T, moduleInstance *Module) melodycontainercontract.Container {
    t.Helper()

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    containerInstance.MustRegister(
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
        melodycontainer.RegisterOptions{},
    )

    moduleInstance.registerServerSentEventHubService(containerRegistrar{Container: containerInstance})
    moduleInstance.registerInternalAuthSignerService(containerRegistrar{Container: containerInstance})

    return containerInstance
}

func TestBuiltAtBoot_BuildsTheInternalAuthSignerOnce(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.buildInternalAuth()
    containerInstance := bootContainerWithInternalAuthSigner(t, moduleInstance)

    if resolveErr := buildServicesBuiltAtBoot(t, containerInstance); nil != resolveErr {
        t.Fatalf("unexpected boot resolution error: %v", resolveErr)
    }

    first := melodycontainer.MustFromResolver[*melodysecurity.HmacEnvelopeSigner](t.Context(), containerInstance, ServiceInternalAuthSigner)
    second := melodycontainer.MustFromResolver[*melodysecurity.HmacEnvelopeSigner](t.Context(), containerInstance, ServiceInternalAuthSigner)
    if first != second {
        t.Fatal("expected one signer for the process")
    }
}

func TestBuiltAtBoot_ACurrentKeyNotBoundToTheSignerApplicationFailsTheBoot(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.buildInternalAuth()
    moduleInstance.internalAuthApplication = "another-service"
    containerInstance := bootContainerWithInternalAuthSigner(t, moduleInstance)

    resolveErr := buildServicesBuiltAtBoot(t, containerInstance)
    for cause := resolveErr; nil != cause; cause = errors.Unwrap(cause) {
        if true == strings.Contains(cause.Error(), "not bound to the signer application") {
            return
        }
    }

    t.Fatalf("expected the boot refused over the unbound key, got %v", resolveErr)
}

/* buildServicesBuiltAtBoot builds what the application's Boot builds, through a boot runtime: the services registered with BuiltAtBoot, in registration order */
func buildServicesBuiltAtBoot(t *testing.T, containerInstance melodycontainercontract.Container) error {
    t.Helper()

    names, modes := builtAtBootNames(containerInstance)
    if 2 != len(names) || subscriber.ServiceCatalogNotificationHub != names[0] || ServiceInternalAuthSigner != names[1] {
        t.Fatalf("expected the hub and the signer registered to be built at boot, got %v", names)
    }

    for _, serviceName := range names {
        if melodycontainer.BootFailureFailsBoot != modes[serviceName] {
            t.Fatalf("expected %s to fail the boot, a refusal being configuration, got %s", serviceName, modes[serviceName])
        }
    }

    bootRuntime, finish := melodyruntime.NewBoot(t.Context(), containerInstance)
    defer func() {
        _ = finish()
    }()

    for _, serviceName := range names {
        if _, buildErr := bootRuntime.Scope().Get(bootRuntime.Context(), serviceName); nil != buildErr {
            return buildErr
        }
    }

    return nil
}

/* kindRecordingSecrets records the kind of the runtime each provider door is asked under */
type kindRecordingSecrets struct {
    melodysecurity.HmacSecretProvider
    kinds []melodycontainercontract.Kind
}

func (instance *kindRecordingSecrets) CurrentKeyId(runtimeInstance melodycontainercontract.Runtime) (string, error) {
    instance.kinds = append(instance.kinds, runtimeInstance.Kind())

    return instance.HmacSecretProvider.CurrentKeyId(runtimeInstance)
}

func (instance *kindRecordingSecrets) Secret(runtimeInstance melodycontainercontract.Runtime, keyId string) ([]byte, bool, error) {
    instance.kinds = append(instance.kinds, runtimeInstance.Kind())

    return instance.HmacSecretProvider.Secret(runtimeInstance, keyId)
}

func (instance *kindRecordingSecrets) ApplicationForKeyId(runtimeInstance melodycontainercontract.Runtime, keyId string) (string, bool, error) {
    instance.kinds = append(instance.kinds, runtimeInstance.Kind())

    return instance.HmacSecretProvider.ApplicationForKeyId(runtimeInstance, keyId)
}

func TestBuiltAtBoot_TheInternalAuthSignerIsJudgedUnderTheBootRuntime(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.buildInternalAuth()
    recording := &kindRecordingSecrets{HmacSecretProvider: moduleInstance.hmacSecrets}
    moduleInstance.hmacSecrets = recording
    containerInstance := bootContainerWithInternalAuthSigner(t, moduleInstance)

    if buildErr := buildServicesBuiltAtBoot(t, containerInstance); nil != buildErr {
        t.Fatalf("unexpected boot build error: %v", buildErr)
    }

    if 3 != len(recording.kinds) {
        t.Fatalf("expected the current key, its binding and its secret read at boot, got %d reads", len(recording.kinds))
    }

    for _, kind := range recording.kinds {
        if melodycontainercontract.KindBoot != kind {
            t.Fatalf("expected every read under the boot runtime, got %s", kind)
        }
    }
}

/* every service the example builds at boot states its mode, the default included, so the composition root reads as the document of the option */
func TestBuiltAtBoot_EveryRegistrationOfTheCompositionRootStatesItsMode(t *testing.T) {
    sourcePaths, globErr := filepath.Glob("*.go")
    if nil != globErr {
        t.Fatalf("glob: %v", globErr)
    }

    stated := 0
    for _, sourcePath := range sourcePaths {
        if true == strings.HasSuffix(sourcePath, "_test.go") {
            continue
        }

        source, readErr := os.ReadFile(sourcePath)
        if nil != readErr {
            t.Fatalf("read %s: %v", sourcePath, readErr)
        }

        for _, line := range strings.Split(string(source), "\n") {
            if false == strings.Contains(line, "BuiltAtBoot: true") {
                continue
            }

            if false == strings.Contains(line, "BootFailure: melodycontainer.BootFailure") {
                t.Fatalf("%s: expected the boot failure mode stated beside BuiltAtBoot, got %q", sourcePath, strings.TrimSpace(line))
            }

            stated = stated + 1
        }
    }

    if 5 != stated {
        t.Fatalf("expected the hub, the signer, the database registry, the catalogue schema and the redis connection, got %d registrations", stated)
    }
}
