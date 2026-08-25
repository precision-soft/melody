package audit

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "io"
    "strings"
    "testing"

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

/* newTestDatabase builds an offline *bun.DB whose schema parsing (table and primary-key metadata)
   works without a live connection; the dialect logs an undiscoverable version and moves on. */
func newTestDatabase() *bun.DB {
    return bun.NewDB(sql.OpenDB(fakeConnector{}), mysqldialect.New())
}

func TestCloneModel_DecouplesPointerSliceAndMapFields(t *testing.T) {
    type entity struct {
        Name   *string
        Tags   []string
        Labels map[string]string
    }

    name := "before"
    model := &entity{
        Name:   &name,
        Tags:   []string{"x"},
        Labels: map[string]string{"k": "before"},
    }

    cloned, cloneErr := cloneModel(model)
    if nil != cloneErr {
        t.Fatalf("clone: %v", cloneErr)
    }

    typedClone := cloned.(*entity)
    *typedClone.Name = "after"
    typedClone.Tags[0] = "y"
    typedClone.Labels["k"] = "after"

    if "before" != *model.Name {
        t.Fatalf("clone aliased the original pointer field; bun would scan the old row in-place over the live model, got %q", *model.Name)
    }
    if "x" != model.Tags[0] {
        t.Fatalf("clone aliased the original slice field, got %q", model.Tags[0])
    }
    if "before" != model.Labels["k"] {
        t.Fatalf("clone aliased the original map field, got %q", model.Labels["k"])
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

    /* A zero primary key is rendered verbatim as "0"; only a nil pointer or a missing key yields "". */
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

/* scriptedDatabase answers the statements an audited write issues over a single stored row and records
   them in order, so a test can assert both what was executed and what the database held. */
type scriptedDatabase struct {
    row        *auditedAccount
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
    /* the dialect probes the server version while bun.NewDB builds; that probe is not part of the audited work */
    if true == strings.Contains(query, "version()") {
        return &scriptedRows{columns: []string{"version"}, values: [][]driver.Value{{"8.0.0"}}}, nil
    }

    instance.database.statements = append(instance.database.statements, query)

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
        if nil != instance.database.row {
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

/* the default: the caller holds the primary key and nothing else, so the delete claims nothing about the fields it never read — and charges the working database no read to write an audit row */
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

/* a delete that matched no row removed nothing, so the trail must not carry a deletion that never happened */
func TestTracker_DeleteRecordsNothingWhenNoRowMatched(t *testing.T) {
    tracker, _, storage := newScriptedTracker(nil)

    if deleteErr := tracker.Delete(context.Background(), "user", "", &auditedAccount{Id: 42}); nil != deleteErr {
        t.Fatalf("a delete that matched no row must not fail: %v", deleteErr)
    }

    if 0 != len(storage.saves) {
        t.Fatalf("expected no audit entry for a delete that removed nothing, got %d", len(storage.saves))
    }
}

/* the opt-in: an entity whose deleted contents must be recoverable pays a select and a row lock for them */
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

    /* the load runs on a clone, so the caller's own model is left as it was passed in */
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

/* the closure's own failure must travel unchanged: wrapping it too would break every errors.Is a caller performs on the named inner errors */
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
