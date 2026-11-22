package repository

import (
    "context"
    "errors"
    "os"
    "regexp"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/.example/persistence"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
)

/* the six constructors over the catalogue take the schema proof and send no statement: a database that would record every query sees none */
func TestCatalogueConstructors_SendNoStatement(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    storage := persistence.NewCatalogStorage(database).WithAccountSeed()
    catalogSchema := &persistence.CatalogSchema{}

    for name, construct := range map[string]func() (any, error){
        "user":            func() (any, error) { return NewUserRepository(storage, catalogSchema) },
        "product":         func() (any, error) { return NewProductRepository(storage, catalogSchema) },
        "category":        func() (any, error) { return NewCategoryRepository(storage, catalogSchema) },
        "currency":        func() (any, error) { return NewCurrencyRepository(storage, catalogSchema) },
        "catalog journal": func() (any, error) { return NewCatalogJournalRepository(storage, catalogSchema) },
        "user session":    func() (any, error) { return NewUserSessionRepository(storage, catalogSchema) },
    } {
        repositoryInstance, constructErr := construct()
        if nil != constructErr || nil == repositoryInstance {
            t.Fatalf("%s: expected the repository constructed, got %v", name, constructErr)
        }
    }

    if queries := recorder.recordedQueries(); 0 != len(queries) {
        t.Fatalf("expected no statement from a constructor, got %v", queries)
    }
}

/* a constructor of the package holds no context of its own: the catalogue is prepared under the runtime of whoever resolves the schema */
func TestCatalogueConstructors_HoldNoBackgroundContext(t *testing.T) {
    constructor := regexp.MustCompile(`(?s)\nfunc New\w+Repository\([^)]*\) \([^)]*\) \{\n(.*?)\n\}\n`)
    for _, name := range []string{"user", "product", "category", "currency", "catalog_journal", "user_session"} {
        source, readErr := os.ReadFile(name + "_repository.go")
        if nil != readErr {
            t.Fatalf("read %s: %v", name, readErr)
        }

        match := constructor.FindSubmatch(source)
        if nil == match {
            t.Fatalf("%s: expected a constructor", name)
        }

        if true == strings.Contains(string(match[1]), "context.Background()") || false == strings.Contains(string(match[0]), "*persistence.CatalogSchema") {
            t.Fatalf("%s: expected a constructor taking the schema proof and no background context, got %s", name, match[0])
        }
    }
}

func TestPrepareCatalog_RefusesANilRuntimeNamingItsDoor(t *testing.T) {
    prepareErr := PrepareCatalog(nil, persistence.NewCatalogStorage(nil))
    if nil == prepareErr || false == strings.Contains(prepareErr.Error(), "the runtime is nil") {
        t.Fatalf("expected the nil runtime refused, got %v", prepareErr)
    }
}

func TestPrepareCatalog_AStorageWithoutADatabaseIsReadyAsItIs(t *testing.T) {
    runtimeInstance := melodyruntime.NewRun(t.Context(), melodycontainer.NewContainer(melodycontainer.ContainerConfig{}).NewScope())

    if prepareErr := PrepareCatalog(runtimeInstance, persistence.NewCatalogStorage(nil)); nil != prepareErr {
        t.Fatalf("expected nothing to prepare, got %v", prepareErr)
    }
}

/* the migration set runs first and under the runtime's context: a runtime that ended refuses before any statement reaches the database */
func TestPrepareCatalog_RunsUnderTheRuntimesContext(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    endedContext, cancel := context.WithCancel(t.Context())
    cancel()

    runtimeInstance := melodyruntime.NewRun(endedContext, melodycontainer.NewContainer(melodycontainer.ContainerConfig{}).NewScope())

    prepareErr := PrepareCatalog(runtimeInstance, persistence.NewCatalogStorage(database))
    if nil == prepareErr || false == errors.Is(prepareErr, context.Canceled) {
        t.Fatalf("expected the ended runtime's refusal, got %v", prepareErr)
    }

    if queries := recorder.recordedQueries(); 0 != len(queries) {
        t.Fatalf("expected no statement under an ended runtime, got %v", queries)
    }
}
