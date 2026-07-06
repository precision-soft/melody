package openapi

import (
    "testing"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

func TestInfoFromResolver_ReturnsEmptyWhenUnregistered(t *testing.T) {
    serviceContainer := container.NewContainer()

    info := InfoFromResolver(serviceContainer)

    if (Info{}) != info {
        t.Fatalf("expected an empty Info when none is registered, got %+v", info)
    }
}

func TestInfoFromResolver_ReturnsRegisteredInfo(t *testing.T) {
    serviceContainer := container.NewContainer()

    expected := Info{Title: "Example API", Version: "1.2.3"}
    serviceContainer.MustRegister(ServiceOpenApiInfo, func(containercontract.Resolver) (Info, error) {
        return expected, nil
    })

    if expected != InfoFromResolver(serviceContainer) {
        t.Fatalf("expected the registered Info to be resolved")
    }
}
