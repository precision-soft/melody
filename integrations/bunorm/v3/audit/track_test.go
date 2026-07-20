package audit

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "testing"

    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
)

type fakeConnector struct{}

func (fakeConnector) Connect(context.Context) (driver.Conn, error) {
    return nil, errors.New("fake connector never connects")
}

func (fakeConnector) Driver() driver.Driver {
    return fakeDriver{}
}

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) {
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
