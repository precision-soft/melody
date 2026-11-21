package bunorm

import (
    "context"
    "errors"
    "reflect"
    "sync"
    "testing"

    "github.com/uptrace/bun"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

var _ interface {
    Manager(name string) (*Manager, error)
    MustManager(name string) *Manager
    Database(name string) (*bun.DB, error)
    MustDatabase(name string) *bun.DB
    DefaultManager() (*Manager, error)
    MustDefaultManager() *Manager
    DefaultDatabase() (*bun.DB, error)
    MustDefaultDatabase() *bun.DB
    MigrationDatabase(name string) (*bun.DB, error)
    Runtime() containercontract.Runtime
    Registry() *ManagerRegistry
} = Bound{}

func TestBound_ForwardsEveryRuntimeDoorOfTheContract(t *testing.T) {
    registryType := reflect.TypeFor[*ManagerRegistry]()
    runtimeType := reflect.TypeFor[containercontract.Runtime]()
    boundType := reflect.TypeFor[Bound]()

    withoutRuntime := map[string]bool{}
    forwarded := map[string]bool{"Runtime": true, "Registry": true}
    for index := 0; index < registryType.NumMethod(); index = index + 1 {
        door := registryType.Method(index)

        /* the method type of a pointer's method set carries the receiver first */
        if 2 > door.Type.NumIn() || runtimeType != door.Type.In(1) {
            withoutRuntime[door.Name] = true

            continue
        }

        viewMethod, found := boundType.MethodByName(door.Name)
        if false == found {
            t.Fatalf("the view does not forward %s", door.Name)
        }

        if viewMethod.Type.NumIn() != door.Type.NumIn()-1 || viewMethod.Type.NumOut() != door.Type.NumOut() || viewMethod.Type.IsVariadic() != door.Type.IsVariadic() {
            t.Fatalf("%s: the view's signature %s is not the registry's %s less the runtime", door.Name, viewMethod.Type, door.Type)
        }

        for parameterIndex := 2; parameterIndex < door.Type.NumIn(); parameterIndex = parameterIndex + 1 {
            if door.Type.In(parameterIndex) != viewMethod.Type.In(parameterIndex-1) {
                t.Fatalf("%s: parameter %d differs", door.Name, parameterIndex)
            }
        }

        for resultIndex := 0; resultIndex < door.Type.NumOut(); resultIndex = resultIndex + 1 {
            if door.Type.Out(resultIndex) != viewMethod.Type.Out(resultIndex) {
                t.Fatalf("%s: result %d differs", door.Name, resultIndex)
            }
        }

        forwarded[door.Name] = true
    }

    if false == reflect.DeepEqual(map[string]bool{"CloseMigrationDatabase": true, "HasProviderDefinition": true, "CloseWithContext": true}, withoutRuntime) {
        t.Fatalf("expected exactly the three doors without a runtime, got %v", withoutRuntime)
    }

    if 11 != len(forwarded) {
        t.Fatalf("expected the nine runtime doors and the two halves, got %v", forwarded)
    }

    if reflect.TypeFor[*Bound]().NumMethod() != boundType.NumMethod() {
        t.Fatalf("expected every method of the view declared on the value")
    }

    for index := 0; index < boundType.NumMethod(); index = index + 1 {
        if false == forwarded[boundType.Method(index).Name] {
            t.Fatalf("the view declares %s, which forwards no door of the registry", boundType.Method(index).Name)
        }
    }
}

type boundProbeContextKey struct{}

type boundRecordedOpen struct {
    door  string
    name  string
    probe any
}

/* boundRecordingProvider records, per open, the door, the definition it opens and the probe value the runtime's context carries, and answers a fresh stub database */
type boundRecordingProvider struct {
    name    string
    mutex   *sync.Mutex
    records *[]boundRecordedOpen
}

func (instance *boundRecordingProvider) record(door string, runtimeInstance containercontract.Runtime) (*bun.DB, error) {
    instance.mutex.Lock()
    *instance.records = append(*instance.records, boundRecordedOpen{door: door, name: instance.name, probe: runtimeInstance.Context().Value(boundProbeContextKey{})})
    instance.mutex.Unlock()

    database, _ := newCloseRaceDatabase()

    return database, nil
}

func (instance *boundRecordingProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.record("Open", runtimeInstance)
}

func (instance *boundRecordingProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.record("OpenForMigration", runtimeInstance)
}

func newBoundRecordingRegistry(t *testing.T, names ...string) (*ManagerRegistry, *[]boundRecordedOpen) {
    t.Helper()

    mutex := &sync.Mutex{}
    records := &[]boundRecordedOpen{}
    definitions := make([]ProviderDefinition, 0, len(names))
    for index, name := range names {
        definitions = append(definitions, ProviderDefinition{Name: name, Provider: &boundRecordingProvider{name: name, mutex: mutex, records: records}, Parameters: testConnectionParameters, IsDefault: 0 == index})
    }

    registry, registryErr := NewManagerRegistry(&fakeLogger{}, definitions)
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }
    t.Cleanup(func() { _ = registry.CloseWithContext(context.Background()) })

    return registry, records
}

func TestBound_EveryMethodReachesItsDoorWithTheBoundRuntimeAndAnswersWhatItAnswered(t *testing.T) {
    registry, records := newBoundRecordingRegistry(t, "main", "manager", "mustManager", "database", "mustDatabase", "migration")
    runtimeInstance := testRuntimeUnder(context.WithValue(context.Background(), boundProbeContextKey{}, "bound"))
    boundRegistry := For(runtimeInstance, registry)

    manager, managerErr := boundRegistry.Manager("manager")
    mustManager := boundRegistry.MustManager("mustManager")
    database, databaseErr := boundRegistry.Database("database")
    mustDatabase := boundRegistry.MustDatabase("mustDatabase")
    migrationDatabase, migrationErr := boundRegistry.MigrationDatabase("migration")
    defaultManager, defaultManagerErr := boundRegistry.DefaultManager()

    for doorName, doorErr := range map[string]error{"Manager": managerErr, "Database": databaseErr, "MigrationDatabase": migrationErr, "DefaultManager": defaultManagerErr} {
        if nil != doorErr {
            t.Fatalf("%s: unexpected error %v", doorName, doorErr)
        }
    }

    expected := []boundRecordedOpen{
        {door: "Open", name: "manager", probe: "bound"},
        {door: "Open", name: "mustManager", probe: "bound"},
        {door: "Open", name: "database", probe: "bound"},
        {door: "Open", name: "mustDatabase", probe: "bound"},
        {door: "OpenForMigration", name: "migration", probe: "bound"},
        {door: "Open", name: "main", probe: "bound"},
    }
    if false == reflect.DeepEqual(expected, *records) {
        t.Fatalf("expected each door opened under the bound runtime on its own definition, got %v", *records)
    }

    if "manager" != manager.DefinitionName() || "mustManager" != mustManager.DefinitionName() || "main" != defaultManager.DefinitionName() {
        t.Fatalf("expected each manager door to answer the manager of its name, got %s, %s, %s", manager.DefinitionName(), mustManager.DefinitionName(), defaultManager.DefinitionName())
    }

    for name, answered := range map[string]*bun.DB{"database": database, "mustDatabase": mustDatabase} {
        if registry.managers[name].Database() != answered {
            t.Fatalf("%s: expected the pool the registry memoized", name)
        }
    }

    if registry.migrationDatabases["migration"] != migrationDatabase {
        t.Fatalf("expected the migration connection the registry keeps")
    }

    defaultDatabase, defaultDatabaseErr := boundRegistry.DefaultDatabase()
    if nil != defaultDatabaseErr || registry.managers["main"].Database() != defaultDatabase || defaultDatabase != boundRegistry.MustDefaultDatabase() || defaultManager != boundRegistry.MustDefaultManager() {
        t.Fatalf("expected the default doors to answer the default manager, got %v", defaultDatabaseErr)
    }

    if _, unknownErr := boundRegistry.Database("unknown"); false == errors.Is(unknownErr, ErrProviderDefinitionNotFound) {
        t.Fatalf("expected the registry's own refusal of an unknown name, got %v", unknownErr)
    }

    if runtimeInstance != boundRegistry.Runtime() || registry != boundRegistry.Registry() {
        t.Fatalf("expected the halves to answer what the view was bound over")
    }
}

func TestBound_AViewOverNoRegistryRefusesEveryDoor(t *testing.T) {
    for _, boundRegistry := range []Bound{{}, For(testRuntime(), nil)} {
        _, managerErr := boundRegistry.Manager("main")
        _, databaseErr := boundRegistry.Database("main")
        _, defaultManagerErr := boundRegistry.DefaultManager()
        _, defaultDatabaseErr := boundRegistry.DefaultDatabase()
        _, migrationErr := boundRegistry.MigrationDatabase("main")

        for doorName, doorErr := range map[string]error{"Manager": managerErr, "Database": databaseErr, "DefaultManager": defaultManagerErr, "DefaultDatabase": defaultDatabaseErr, "MigrationDatabase": migrationErr} {
            if nil == doorErr || "manager registry is nil" != doorErr.Error() {
                t.Fatalf("%s: expected the absent registry refused, got %v", doorName, doorErr)
            }
        }

        for doorName, door := range map[string]func(){
            "MustManager":         func() { boundRegistry.MustManager("main") },
            "MustDatabase":        func() { boundRegistry.MustDatabase("main") },
            "MustDefaultManager":  func() { boundRegistry.MustDefaultManager() },
            "MustDefaultDatabase": func() { boundRegistry.MustDefaultDatabase() },
        } {
            recovered := func() (recoveredErr error) {
                defer func() {
                    recoveredErr, _ = recover().(error)
                }()

                door()

                return nil
            }()

            if nil == recovered || "manager registry is nil" != recovered.Error() {
                t.Fatalf("%s: expected a panic carrying the view's refusal, got %v", doorName, recovered)
            }
        }
    }
}

/* a package variable, so the direct call is made as the view makes it */
var boundAllocationRegistry *ManagerRegistry

func TestBound_BindingAddsNoAllocationToTheRegistrysCall(t *testing.T) {
    registry, _ := newBoundRecordingRegistry(t, "main")
    runtimeInstance := testRuntime()
    boundAllocationRegistry = registry

    if _, managerErr := registry.Manager(runtimeInstance, "main"); nil != managerErr {
        t.Fatalf("manager: %v", managerErr)
    }

    directAllocations := testing.AllocsPerRun(1000, func() {
        _, _ = boundAllocationRegistry.Manager(runtimeInstance, "main")
    })
    boundAllocations := testing.AllocsPerRun(1000, func() {
        _, _ = For(runtimeInstance, boundAllocationRegistry).Manager("main")
    })
    if directAllocations != boundAllocations {
        t.Fatalf("expected a bind and a call to allocate what the direct call does, %v, got %v", directAllocations, boundAllocations)
    }
}

func TestFromRuntime_BindsTheRegistryRegisteredUnderTheApplicationsName(t *testing.T) {
    first, _ := newBoundRecordingRegistry(t, "main")
    second, _ := newBoundRecordingRegistry(t, "main")

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    for serviceName, registry := range map[string]*ManagerRegistry{"app.database.first": first, "app.database.second": second} {
        if registerErr := serviceContainer.Register(
            serviceName,
            func(resolver containercontract.Resolver) (*ManagerRegistry, error) {
                return registry, nil
            },
            container.RegisterOptions{TypeRegistrationDisabled: true},
        ); nil != registerErr {
            t.Fatalf("register: %v", registerErr)
        }
    }

    runtimeInstance := melodyruntime.NewRun(context.Background(), serviceContainer.NewScope())
    if bound := FromRuntime(runtimeInstance, "app.database.first"); first != bound.Registry() || runtimeInstance != bound.Runtime() {
        t.Fatalf("expected the first registry bound to the runtime")
    }

    if second != FromRuntime(runtimeInstance, "app.database.second").Registry() {
        t.Fatalf("expected the registry of the name asked for")
    }

    panicked := func() (recovered bool) {
        defer func() {
            recovered = nil != recover()
        }()

        FromRuntime(runtimeInstance, "app.database.missing")

        return false
    }()
    if false == panicked {
        t.Fatalf("expected a name nothing registered to panic")
    }
}

func TestBound_ADoneRuntimeIsRefusedBeforeAnOpen(t *testing.T) {
    registry, records := newBoundRecordingRegistry(t, "main")

    doneCause := errors.New("the request ended")
    doneContext, cancel := context.WithCancelCause(context.Background())
    cancel(doneCause)

    if _, databaseErr := For(testRuntimeUnder(doneContext), registry).Database("main"); false == errors.Is(databaseErr, doneCause) {
        t.Fatalf("expected the caller's cause, got %v", databaseErr)
    }

    if 0 != len(*records) || 0 != len(registry.managers) {
        t.Fatalf("expected nothing opened nor memoized under the done runtime, got %v", *records)
    }

    if _, databaseErr := For(testRuntime(), registry).Database("main"); nil != databaseErr || 1 != len(*records) {
        t.Fatalf("expected a live view to open, got %v after %d opens", databaseErr, len(*records))
    }
}
