package audit

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "io"
    "strings"
    "testing"
    "time"

    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
)

type fakeConnector struct{}

func (instance fakeConnector) Connect(context.Context) (driver.Conn, error) {
    return nil, errors.New("fake connector never connects")
}

func (instance fakeConnector) Driver() driver.Driver {
    return fakeDriver{}
}

type fakeDriver struct{}

func (instance fakeDriver) Open(string) (driver.Conn, error) {
    return nil, errors.New("fake driver never opens")
}

func newTestDatabase() *bun.DB {
    return bun.NewDB(sql.OpenDB(fakeConnector{}), mysqldialect.New())
}

func TestBeforeImageModel_HoldsThePrimaryKeyAndNothingElseOfTheModel(t *testing.T) {
    type entity struct {
        bun.BaseModel `bun:"table:before_image_entity"`

        Id     int64             `bun:"id,pk"`
        Tenant string            `bun:"tenant,pk"`
        Name   *string           `bun:"name"`
        Tags   []string          `bun:"tags"`
        Labels map[string]string `bun:"labels"`
    }

    name := "new"
    model := &entity{Id: 7, Tenant: "acme", Name: &name, Tags: []string{"x"}, Labels: map[string]string{"k": "new"}}

    image, imageErr := beforeImageModel(newTestDatabase(), model)
    if nil != imageErr {
        t.Fatalf("before image: %v", imageErr)
    }

    typedImage := image.(*entity)
    if 7 != typedImage.Id || "acme" != typedImage.Tenant {
        t.Fatalf("expected the primary key carried over, got %d %q", typedImage.Id, typedImage.Tenant)
    }

    if nil != typedImage.Name || nil != typedImage.Tags || nil != typedImage.Labels {
        t.Fatalf("expected nothing else of the model carried over, got %+v", typedImage)
    }

    type pointerKeyEntity struct {
        bun.BaseModel `bun:"table:before_image_pointer_key"`

        Id   *string `bun:"id,pk"`
        Name string  `bun:"name"`
    }

    key := "ABC"
    pointerModel := &pointerKeyEntity{Id: &key, Name: "new"}

    pointerImage, pointerImageErr := beforeImageModel(newTestDatabase(), pointerModel)
    if nil != pointerImageErr {
        t.Fatalf("before image of a pointer key: %v", pointerImageErr)
    }

    typedPointerImage := pointerImage.(*pointerKeyEntity)
    if nil == typedPointerImage.Id || "ABC" != *typedPointerImage.Id || "" != typedPointerImage.Name {
        t.Fatalf("expected the pointer key alone carried over, got %+v", typedPointerImage)
    }

    *typedPointerImage.Id = "abc"
    if "ABC" != *pointerModel.Id {
        t.Fatalf("expected a read into the image to leave the caller's key alone, got %q", *pointerModel.Id)
    }

    for _, refused := range []any{nil, entity{}, (*entity)(nil), new(int)} {
        if _, refusedErr := beforeImageModel(newTestDatabase(), refused); nil == refusedErr {
            t.Fatalf("expected %T refused as no pointer to a struct", refused)
        }
    }
}

func TestEntityIdFromModel_DerivesPrimaryKey(t *testing.T) {
    database := newTestDatabase()

    type single struct {
        bun.BaseModel `bun:"table:single"`

        Id   int64  `bun:"id,pk,autoincrement"`
        Name string `bun:"name"`
    }
    if got := entityIdFromModel(database, &single{Id: 42, Name: "x"}); "42" != got {
        t.Fatalf("single pk: got %q, want 42", got)
    }

    if got := entityIdFromModel(database, &single{Id: 0, Name: "x"}); "0" != got {
        t.Fatalf("zero pk: got %q, want 0", got)
    }

    type pointerKey struct {
        bun.BaseModel `bun:"table:pointer_key"`

        Id *uint64 `bun:"id,pk"`
    }
    id := uint64(7)
    if got := entityIdFromModel(database, &pointerKey{Id: &id}); "7" != got {
        t.Fatalf("pointer pk: got %q, want 7", got)
    }
    if got := entityIdFromModel(database, &pointerKey{Id: nil}); "" != got {
        t.Fatalf("nil pointer pk: got %q, want empty", got)
    }

    type composite struct {
        bun.BaseModel `bun:"table:composite"`

        A int64 `bun:"a,pk"`
        B int64 `bun:"b,pk"`
    }
    if got := entityIdFromModel(database, &composite{A: 1, B: 2}); "1:2" != got {
        t.Fatalf("composite pk: got %q, want 1:2", got)
    }

    type noKey struct {
        bun.BaseModel `bun:"table:no_key"`

        Name string `bun:"name"`
    }
    if got := entityIdFromModel(database, &noKey{Name: "x"}); "" != got {
        t.Fatalf("no pk: got %q, want empty", got)
    }

    if got := entityIdFromModel(database, "not a struct pointer"); "" != got {
        t.Fatalf("non-struct: got %q, want empty", got)
    }
}

type auditedAccount struct {
    bun.BaseModel `bun:"table:audited_account"`

    Id      int64  `bun:"id,pk"`
    Name    string `bun:"name"`
    Email   string `bun:"email"`
    Balance int64  `bun:"balance"`
}

var auditedAccountColumnList = []string{"id", "name", "email", "balance"}

type scriptedDatabase struct {
    row        *auditedAccount
    rowColumns []string
    rowValues  []driver.Value
    statements []string
    commitErr  error
}

type scriptedConnector struct {
    database *scriptedDatabase
}

func (instance *scriptedConnector) Connect(context.Context) (driver.Conn, error) {
    return &scriptedConnection{database: instance.database}, nil
}

func (instance *scriptedConnector) Driver() driver.Driver {
    return scriptedDriver{}
}

type scriptedDriver struct{}

func (instance scriptedDriver) Open(string) (driver.Conn, error) {
    return nil, errors.New("scripted driver opens only through its connector")
}

type scriptedConnection struct {
    database *scriptedDatabase
}

func (instance *scriptedConnection) Prepare(string) (driver.Stmt, error) {
    return nil, errors.New("scripted connection prepares no statements")
}

func (instance *scriptedConnection) Close() error {
    return nil
}

func (instance *scriptedConnection) Begin() (driver.Tx, error) {
    return scriptedTransaction{commitErr: instance.database.commitErr}, nil
}

func (instance *scriptedConnection) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
    return scriptedTransaction{commitErr: instance.database.commitErr}, nil
}

func (instance *scriptedConnection) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
    if true == strings.Contains(query, "version()") {
        return &scriptedRows{columns: []string{"version"}, values: [][]driver.Value{{"8.0.0"}}}, nil
    }

    instance.database.statements = append(instance.database.statements, query)

    if nil != instance.database.rowColumns {
        return &scriptedRows{columns: instance.database.rowColumns, values: [][]driver.Value{instance.database.rowValues}}, nil
    }

    if nil == instance.database.row {
        return &scriptedRows{columns: auditedAccountColumnList}, nil
    }

    row := instance.database.row

    return &scriptedRows{
        columns: auditedAccountColumnList,
        values:  [][]driver.Value{{row.Id, row.Name, row.Email, row.Balance}},
    }, nil
}

func (instance *scriptedConnection) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
    instance.database.statements = append(instance.database.statements, query)

    if true == strings.HasPrefix(strings.ToUpper(strings.TrimSpace(query)), "DELETE") {
        affected := int64(0)
        if nil != instance.database.row || nil != instance.database.rowColumns {
            affected = 1
        }

        instance.database.row = nil

        return scriptedResult{affected: affected}, nil
    }

    return scriptedResult{affected: 1}, nil
}

type scriptedTransaction struct {
    commitErr error
}

func (instance scriptedTransaction) Commit() error {
    return instance.commitErr
}

func (instance scriptedTransaction) Rollback() error {
    return nil
}

type scriptedResult struct {
    affected int64
}

func (instance scriptedResult) LastInsertId() (int64, error) {
    return 0, nil
}

func (instance scriptedResult) RowsAffected() (int64, error) {
    return instance.affected, nil
}

type scriptedRows struct {
    columns []string
    values  [][]driver.Value
    index   int
}

func (instance *scriptedRows) Columns() []string {
    return instance.columns
}

func (instance *scriptedRows) Close() error {
    return nil
}

func (instance *scriptedRows) Next(destination []driver.Value) error {
    if instance.index >= len(instance.values) {
        return io.EOF
    }

    copy(destination, instance.values[instance.index])
    instance.index++

    return nil
}

func newScriptedTracker(storedRow *auditedAccount, options ...EntityOptions) (*Tracker, *scriptedDatabase, *fakeStorage) {
    scripted := &scriptedDatabase{row: storedRow}
    database := bun.NewDB(sql.OpenDB(&scriptedConnector{database: scripted}), mysqldialect.New())
    storage := &fakeStorage{}
    registry := NewRegistry("melody_audit")

    for _, entityOptions := range options {
        registry.Register("user", entityOptions)
    }

    return NewTracker(database, NewRecorderWithStorage(storage, registry)), scripted, storage
}

func TestTracker_DeleteClaimsNothingItDidNotRead(t *testing.T) {
    tracker, scripted, storage := newScriptedTracker(&auditedAccount{
        Id:      42,
        Name:    "real name",
        Email:   "real@example.com",
        Balance: 9999,
    })

    model := &auditedAccount{Id: 42, Name: "FABRICATED", Email: "fake@example.com", Balance: 1}

    if deleteErr := tracker.Delete(context.Background(), "user", "", model); nil != deleteErr {
        t.Fatalf("delete: %v", deleteErr)
    }

    for _, statement := range scripted.statements {
        if true == strings.Contains(statement, "SELECT") {
            t.Fatalf("the default delete must not read the row, got %q", statement)
        }
    }

    if 1 != len(storage.saves) {
        t.Fatalf("expected one audit entry, got %d", len(storage.saves))
    }

    entry := storage.saves[0].entries[0]
    if "42" != entry.EntityId {
        t.Fatalf("expected the entity id derived from the pk, got %q", entry.EntityId)
    }

    for _, forbidden := range []string{"FABRICATED", "fake@example.com", `"old":""`, `"old":0`} {
        if true == strings.Contains(entry.Changes, forbidden) {
            t.Fatalf("the trail must assert nothing about unread fields, found %s in %s", forbidden, entry.Changes)
        }
    }

    if "[]" != entry.Changes {
        t.Fatalf("expected an empty change set rather than the json literal null, got %q", entry.Changes)
    }
}

func TestTracker_DeleteRecordsNothingWhenNoRowMatched(t *testing.T) {
    tracker, _, storage := newScriptedTracker(nil)

    if deleteErr := tracker.Delete(context.Background(), "user", "", &auditedAccount{Id: 42}); nil != deleteErr {
        t.Fatalf("a delete that matched no row must not fail: %v", deleteErr)
    }

    if 0 != len(storage.saves) {
        t.Fatalf("expected no audit entry for a delete that removed nothing, got %d", len(storage.saves))
    }
}

func TestTracker_DeleteCapturesTheStoredRowWhenTheEntityOptsIn(t *testing.T) {
    tracker, scripted, storage := newScriptedTracker(
        &auditedAccount{Id: 42, Name: "real name", Email: "real@example.com", Balance: 9999},
        EntityOptions{CaptureDeleteBeforeImage: true},
    )

    model := &auditedAccount{Id: 42, Name: "FABRICATED", Email: "fake@example.com", Balance: 1}

    if deleteErr := tracker.Delete(context.Background(), "user", "", model); nil != deleteErr {
        t.Fatalf("delete: %v", deleteErr)
    }

    if 2 != len(scripted.statements) {
        t.Fatalf("expected a select followed by a delete, got %d statements: %v", len(scripted.statements), scripted.statements)
    }
    if false == strings.Contains(scripted.statements[0], "SELECT") || false == strings.Contains(scripted.statements[0], "FOR UPDATE") {
        t.Fatalf("expected the row to be loaded and locked before the delete, got %q", scripted.statements[0])
    }

    entry := storage.saves[0].entries[0]
    for _, expected := range []string{`"old":"real name"`, `"old":"real@example.com"`, `"old":9999`} {
        if false == strings.Contains(entry.Changes, expected) {
            t.Fatalf("the before-image must carry the stored row, expected %s in %s", expected, entry.Changes)
        }
    }
    if true == strings.Contains(entry.Changes, "FABRICATED") {
        t.Fatalf("a caller-written before-image must not reach the trail: %s", entry.Changes)
    }

    if "FABRICATED" != model.Name {
        t.Fatalf("the caller model must not be overwritten by the load, got %q", model.Name)
    }
}

func TestTracker_DeleteFailsWhenAnOptedInRowIsAlreadyAbsent(t *testing.T) {
    tracker, scripted, storage := newScriptedTracker(nil, EntityOptions{CaptureDeleteBeforeImage: true})

    deleteErr := tracker.Delete(context.Background(), "user", "", &auditedAccount{Id: 42})
    if nil == deleteErr {
        t.Fatalf("an opted-in delete must fail when the row it must capture is gone")
    }

    for _, statement := range scripted.statements {
        if true == strings.Contains(statement, "DELETE") {
            t.Fatalf("no delete may run once the row could not be loaded, got %q", statement)
        }
    }

    if 0 != len(storage.saves) {
        t.Fatalf("expected no audit entry, got %d", len(storage.saves))
    }
}

func TestResolveEntityId_PrefersCallerSuppliedId(t *testing.T) {
    tracker := &Tracker{database: newTestDatabase()}

    type single struct {
        bun.BaseModel `bun:"table:single"`

        Id int64 `bun:"id,pk,autoincrement"`
    }

    if got := tracker.resolveEntityId("caller-42", &single{Id: 99}); "caller-42" != got {
        t.Fatalf("a caller-supplied id must win, got %q", got)
    }
    if got := tracker.resolveEntityId("", &single{Id: 99}); "99" != got {
        t.Fatalf("an empty id must be derived from the pk, got %q, want 99", got)
    }
}

func TestTracker_RefusesACallerBoundContext(t *testing.T) {
    database := newTestDatabase()
    tracker := NewTracker(database, NewRecorderWithStorage(&fakeStorage{}, nil))

    boundCtx := WithDatabase(context.Background(), database)

    insertErr := tracker.Insert(boundCtx, "parityAccount", "1", &parityAccount{Id: 1})
    if nil == insertErr {
        t.Fatalf("expected the caller-bound context to be refused")
    }

    if false == strings.Contains(insertErr.Error(), "refuses a context bound with WithDatabase") {
        t.Fatalf("expected the refusal to name the misuse and the alternative, got: %v", insertErr)
    }
}

func TestTracker_WrapsTheCommitMarginWithTheEntity(t *testing.T) {
    commitErr := errors.New("driver: commit deadlock")
    scripted := &scriptedDatabase{commitErr: commitErr}
    database := bun.NewDB(sql.OpenDB(&scriptedConnector{database: scripted}), mysqldialect.New())
    tracker := NewTracker(database, NewRecorderWithStorage(&fakeStorage{}, nil))

    insertErr := tracker.Insert(context.Background(), "parityAccount", "1", &parityAccount{Id: 1, Email: "a@example.com"})
    if nil == insertErr {
        t.Fatalf("expected the commit failure to surface")
    }

    if false == strings.Contains(insertErr.Error(), "audited write transaction failed") {
        t.Fatalf("expected the margin wrap, got: %v", insertErr)
    }

    if false == errors.Is(insertErr, commitErr) {
        t.Fatalf("expected the commit cause to stay reachable by identity, got: %v", insertErr)
    }
}

func TestTracker_LeavesTheUnitOfWorkErrorUnwrapped(t *testing.T) {
    scripted := &scriptedDatabase{}
    database := bun.NewDB(sql.OpenDB(&scriptedConnector{database: scripted}), mysqldialect.New())
    saveErr := errors.New("storage refused")
    tracker := NewTracker(database, NewRecorderWithStorage(&fakeStorage{failWith: saveErr}, nil))

    insertErr := tracker.Insert(context.Background(), "parityAccount", "1", &parityAccount{Id: 1})
    if saveErr != insertErr {
        t.Fatalf("expected the unit-of-work error to keep its identity, got: %v", insertErr)
    }
}

func TestEntityIdFromModel_EscapesTheCompositeJoin(t *testing.T) {
    database := newTestDatabase()

    type composite struct {
        bun.BaseModel `bun:"table:composite"`

        Left  string `bun:"left_part,pk"`
        Right string `bun:"right_part,pk"`
    }

    ambiguousLeft := entityIdFromModel(database, &composite{Left: "a:b", Right: "c"})
    ambiguousRight := entityIdFromModel(database, &composite{Left: "a", Right: "b:c"})

    if ambiguousLeft == ambiguousRight {
        t.Fatalf("expected the two composite keys to derive distinct ids, both derived %q", ambiguousLeft)
    }

    if `a\:b:c` != ambiguousLeft {
        t.Fatalf("expected the colon inside a part to be escaped, got %q", ambiguousLeft)
    }
}

func TestEntityIdFromModel_ASingleColumnKeyCarryingAColonIsNotEscaped(t *testing.T) {
    database := newTestDatabase()

    type single struct {
        bun.BaseModel `bun:"table:single"`

        Id string `bun:"id,pk"`
    }

    if derived := entityIdFromModel(database, &single{Id: "tenant:42"}); "tenant:42" != derived {
        t.Fatalf("expected the single-column key unescaped, got %q", derived)
    }

    if derived := entityIdFromModel(database, &single{Id: `a\b`}); `a\b` != derived {
        t.Fatalf("expected the single-column key unescaped, got %q", derived)
    }
}

type nonComparableStorageError struct {
    fields []string
}

func (instance nonComparableStorageError) Error() string {
    return "storage refused " + strings.Join(instance.fields, ",")
}

func TestTracker_AStorageErrorOfANonComparableTypeIsReportedNotPanicked(t *testing.T) {
    scripted := &scriptedDatabase{}
    database := bun.NewDB(sql.OpenDB(&scriptedConnector{database: scripted}), mysqldialect.New())
    tracker := NewTracker(database, NewRecorderWithStorage(&fakeStorage{failWith: nonComparableStorageError{fields: []string{"iban"}}}, nil))

    defer func() {
        if recovered := recover(); nil != recovered {
            t.Fatalf("expected the failure reported, it panicked: %v", recovered)
        }
    }()

    insertErr := tracker.Insert(context.Background(), "parityAccount", "1", &parityAccount{Id: 1})

    var storageErr nonComparableStorageError
    if false == errors.As(insertErr, &storageErr) {
        t.Fatalf("expected the storage's failure reachable, got: %v", insertErr)
    }
}

type nonComparableCyclicStorageError struct {
    parts []string
}

func (instance nonComparableCyclicStorageError) Error() string {
    return "non comparable cyclic storage failure"
}

func (instance nonComparableCyclicStorageError) Unwrap() error {
    return instance
}

func TestTracker_AStorageErrorOnACyclicChainIsReported(t *testing.T) {
    scripted := &scriptedDatabase{}
    database := bun.NewDB(sql.OpenDB(&scriptedConnector{database: scripted}), mysqldialect.New())
    tracker := NewTracker(database, NewRecorderWithStorage(&fakeStorage{failWith: nonComparableCyclicStorageError{parts: []string{"storage"}}}, nil))

    insertReturned := make(chan error, 1)
    go func() {
        insertReturned <- tracker.Insert(context.Background(), "parityAccount", "1", &parityAccount{Id: 1})
    }()

    select {
    case insertErr := <-insertReturned:
        if nil == insertErr {
            t.Fatal("expected the storage's failure")
        }
    case <-time.After(5 * time.Second):
        t.Fatal("expected the insert to return on an error chain that reaches itself")
    }
}

type auditedLabelled struct {
    bun.BaseModel `bun:"table:audited_labelled"`

    Id     int64             `bun:"id,pk"`
    Labels map[string]string `bun:"labels"`
}

func TestTracker_UpdateReadsTheBeforeImageIntoAModelHoldingNothingOfTheNewOne(t *testing.T) {
    scripted := &scriptedDatabase{rowColumns: []string{"id", "labels"}, rowValues: []driver.Value{int64(42), []byte(`{"tier":"gold"}`)}}
    database := bun.NewDB(sql.OpenDB(&scriptedConnector{database: scripted}), mysqldialect.New())
    storage := &fakeStorage{}
    tracker := NewTracker(database, NewRecorderWithStorage(storage, NewRegistry("melody_audit")))

    model := &auditedLabelled{Id: 42, Labels: map[string]string{"tier": "gold", "region": "eu"}}

    if updateErr := tracker.Update(context.Background(), "labelled", "", model); nil != updateErr {
        t.Fatalf("update: %v", updateErr)
    }

    if 1 != len(storage.saves) {
        t.Fatalf("expected one audit entry, got %d", len(storage.saves))
    }

    changes := storage.saves[0].entries[0].Changes
    if false == strings.Contains(changes, "region") || true == strings.Contains(changes, `"old":{"region"`) || false == strings.Contains(changes, `"old":{"tier":"gold"}`) {
        t.Fatalf("expected the added key recorded against the stored map alone, got %s", changes)
    }
}

func TestTracker_DeleteReadsTheBeforeImageIntoAModelHoldingNothingOfThePassedOne(t *testing.T) {
    scripted := &scriptedDatabase{rowColumns: []string{"id", "labels"}, rowValues: []driver.Value{int64(42), []byte(`{"tier":"gold"}`)}}
    database := bun.NewDB(sql.OpenDB(&scriptedConnector{database: scripted}), mysqldialect.New())
    storage := &fakeStorage{}
    registry := NewRegistry("melody_audit").Register("labelled", EntityOptions{CaptureDeleteBeforeImage: true})
    tracker := NewTracker(database, NewRecorderWithStorage(storage, registry))

    model := &auditedLabelled{Id: 42, Labels: map[string]string{"tier": "gold", "region": "eu"}}

    if deleteErr := tracker.Delete(context.Background(), "labelled", "", model); nil != deleteErr {
        t.Fatalf("delete: %v", deleteErr)
    }

    if 1 != len(storage.saves) {
        t.Fatalf("expected one audit entry, got %d", len(storage.saves))
    }

    changes := storage.saves[0].entries[0].Changes
    if false == strings.Contains(changes, `"old":{"tier":"gold"}`) || true == strings.Contains(changes, "region") {
        t.Fatalf("expected the stored map alone in the deleted row's image, got %s", changes)
    }
}
