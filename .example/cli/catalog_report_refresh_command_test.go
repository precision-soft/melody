package cli

import (
    "strings"
    "testing"

    "github.com/precision-soft/melody/.example/repository"
    "github.com/precision-soft/melody/.example/service"
    melodyclock "github.com/precision-soft/melody/clock"
    melodycontainer "github.com/precision-soft/melody/container"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
)

func newCatalogReportContainer() melodycontainercontract.Container {
    serviceContainer := melodycontainer.NewContainer()

    reportService := service.NewCatalogReportService(
        melodyclock.NewFrozenClock(fixtureCliTime),
        nil,
        repository.NewInMemoryProductRepository(),
        &stubJournalRepository{},
    )

    registerCliTestService(serviceContainer, service.ServiceCatalogReportService, reportService)

    return serviceContainer
}

func TestCatalogReportRefreshRendersTheReadingAsATable(t *testing.T) {
    serviceContainer := newCatalogReportContainer()

    rendered, runErr := runCliCommand(
        NewCatalogReportRefreshCommand(),
        newCliTestRuntime(serviceContainer),
        nil,
    )
    if nil != runErr {
        t.Fatalf("expected the refresh to succeed, got %v", runErr)
    }

    for _, marker := range []string{"REPORT", "RECORDED_AT", "2026-08-14T12:00:00Z"} {
        if false == strings.Contains(rendered, marker) {
            t.Fatalf("expected the table to carry %q, got %q", marker, rendered)
        }
    }
}

func TestCatalogReportRefreshRendersTheReadingAsJson(t *testing.T) {
    serviceContainer := newCatalogReportContainer()

    rendered, runErr := runCliCommand(
        NewCatalogReportRefreshCommand(),
        newCliTestRuntime(serviceContainer),
        []string{"--format=json"},
    )
    if nil != runErr {
        t.Fatalf("expected the refresh to succeed, got %v", runErr)
    }

    for _, marker := range []string{`"command":"catalog:report:refresh"`, `"recordedAt":"2026-08-14T12:00:00Z"`} {
        if false == strings.Contains(rendered, marker) {
            t.Fatalf("expected the envelope to carry %q, got %q", marker, rendered)
        }
    }
}
