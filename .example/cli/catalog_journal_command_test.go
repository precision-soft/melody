package cli

import (
    "encoding/json"
    "strings"
    "testing"

    "github.com/precision-soft/melody/.example/repository"
    melodycontainer "github.com/precision-soft/melody/container"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
)

func TestCatalogJournalRefusesWithoutADatabase(t *testing.T) {
    serviceContainer := melodycontainer.NewContainer()

    _, runErr := runCliCommand(
        NewCatalogJournalCommand(),
        newCliTestRuntime(serviceContainer),
        nil,
    )
    if nil == runErr {
        t.Fatal("expected the missing journal to be refused")
    }
    if false == strings.Contains(runErr.Error(), "no journal database configured") {
        t.Fatalf("expected the refusal to say why, got %v", runErr)
    }
}

func newJournalContainerWithEntries() melodycontainercontract.Container {
    serviceContainer := melodycontainer.NewContainer()

    journalRepository := &stubJournalRepository{}
    for _, subjectId := range []string{"prod-1", "prod-2", "prod-3"} {
        journalRepository.entries = append(journalRepository.entries, &repository.CatalogJournalEntry{
            Id:         int64(len(journalRepository.entries) + 1),
            Actor:      "editor",
            Action:     repository.CatalogJournalActionUpdated,
            Subject:    "product",
            SubjectId:  subjectId,
            RecordedAt: fixtureCliTime,
        })
    }

    registerCliTestService[repository.CatalogJournalRepository](
        serviceContainer,
        repository.ServiceCatalogJournalRepository,
        journalRepository,
    )

    return serviceContainer
}

func TestCatalogJournalHonoursTheStandardLimit(t *testing.T) {
    serviceContainer := newJournalContainerWithEntries()

    rendered, runErr := runCliCommand(
        NewCatalogJournalCommand(),
        newCliTestRuntime(serviceContainer),
        []string{"--format=json", "--limit=1"},
    )
    if nil != runErr {
        t.Fatalf("expected the listing to succeed, got %v", runErr)
    }

    document := struct {
        Data struct {
            Items []catalogJournalListItem `json:"items"`
            Total int                      `json:"total"`
            Limit int                      `json:"limit"`
        } `json:"data"`
    }{}
    if decodeErr := json.Unmarshal([]byte(rendered), &document); nil != decodeErr {
        t.Fatalf("expected one json envelope, got %q: %v", rendered, decodeErr)
    }

    if 1 != document.Data.Limit || 1 != len(document.Data.Items) {
        t.Fatalf("expected the standard limit to window the answer, got %+v", document.Data)
    }
    if 3 != document.Data.Total {
        t.Fatalf("expected the total to keep counting everything, got %d", document.Data.Total)
    }
    if int64(3) != document.Data.Items[0].Id {
        t.Fatalf("expected the newest entry first, got %+v", document.Data.Items[0])
    }
}

func TestCatalogJournalTableAnswersEverythingByDefault(t *testing.T) {
    serviceContainer := newJournalContainerWithEntries()

    rendered, runErr := runCliCommand(
        NewCatalogJournalCommand(),
        newCliTestRuntime(serviceContainer),
        nil,
    )
    if nil != runErr {
        t.Fatalf("expected the listing to succeed, got %v", runErr)
    }

    for _, marker := range []string{"JOURNAL: 3 total", "ACTOR", "RECORDED_AT", "prod-3"} {
        if false == strings.Contains(rendered, marker) {
            t.Fatalf("expected the table to carry %q, got %q", marker, rendered)
        }
    }
    if true == strings.Contains(rendered, "shown") {
        t.Fatalf("expected no window suffix without a limit, got %q", rendered)
    }
}
