package audit

import (
    "context"
    "reflect"
    "strings"
    "testing"
)

/* the transaction table name is fixed in the model tag and cannot carry the major, so the constant and the tag must say the same thing: a drift between them means every read goes to a table the writes never fill */
func TestDefaultTransactionTable_IsTheTableTheModelDeclares(t *testing.T) {
    if "melody_audit_transaction" != DefaultTransactionTable {
        t.Fatalf("expected the declared table name, got %q", DefaultTransactionTable)
    }

    baseModelField, exists := reflect.TypeOf(Transaction{}).FieldByName("BaseModel")
    if false == exists {
        t.Fatal("expected the model to declare a bun base model")
    }

    if false == strings.Contains(baseModelField.Tag.Get("bun"), "table:"+DefaultTransactionTable) {
        t.Fatalf("expected the model tag to name the same table as the constant, got %q", baseModelField.Tag.Get("bun"))
    }
}

/* a trail written outside any opened transaction is the ordinary case — a single save, a migration — so the reader answers zero rather than refusing, and the entry simply carries no transaction id */
func TestTransactionIdFromContext_AnswersZeroWhenNoTransactionWasOpened(t *testing.T) {
    if 0 != transactionIdFromContext(context.Background()) {
        t.Fatalf("expected no transaction id on a bare context, got %d", transactionIdFromContext(context.Background()))
    }

    /* the key is a private zero-size struct type, so a value another package stored under its own key cannot be read back as a transaction id */
    type foreignTransactionKey struct{}

    foreign := context.WithValue(context.Background(), foreignTransactionKey{}, int64(7))
    if 0 != transactionIdFromContext(foreign) {
        t.Fatalf("expected a foreign key to be invisible, got %d", transactionIdFromContext(foreign))
    }

    /* a value of the wrong type under the right key reads as absent rather than panicking on the assertion */
    wrongType := context.WithValue(context.Background(), transactionContextKey{}, "7")
    if 0 != transactionIdFromContext(wrongType) {
        t.Fatalf("expected a non-int64 value to read as absent, got %d", transactionIdFromContext(wrongType))
    }

    if 7 != transactionIdFromContext(context.WithValue(context.Background(), transactionContextKey{}, int64(7))) {
        t.Fatal("expected a stored transaction id to read back")
    }
}

/* the extras are encoded BEFORE the insert, so extras that cannot be encoded must refuse without opening a transaction at all: the caller's context comes back untouched and the id is zero, or a later trail row would name a transaction that was never written */
func TestBeginTransaction_RefusesExtrasItCanNotEncodeWithoutOpeningAnything(t *testing.T) {
    ctx := context.Background()

    answeredCtx, id, beginErr := BeginTransaction(ctx, newTestDatabase(), "user-42", map[string]any{"channel": make(chan int)})
    if nil == beginErr {
        t.Fatal("expected unencodable extras to be refused")
    }

    if false == strings.Contains(beginErr.Error(), "could not encode audit transaction extras") {
        t.Fatalf("expected the encoding refusal, got %v", beginErr)
    }

    if 0 != id {
        t.Fatalf("expected no transaction id on a refusal, got %d", id)
    }

    if ctx != answeredCtx {
        t.Fatal("expected the caller's context to come back untouched")
    }

    if 0 != transactionIdFromContext(answeredCtx) {
        t.Fatalf("expected no transaction id to be bound on a refusal, got %d", transactionIdFromContext(answeredCtx))
    }
}

/* an insert that cannot reach the database is wrapped rather than handed back raw, and it too must leave the caller's context unbound */
func TestBeginTransaction_WrapsAnInsertItCouldNotRun(t *testing.T) {
    ctx := context.Background()

    answeredCtx, id, beginErr := BeginTransaction(ctx, newTestDatabase(), "user-42", nil)
    if nil == beginErr {
        t.Fatal("expected an unreachable database to refuse")
    }

    if false == strings.Contains(beginErr.Error(), "could not open the audit transaction") {
        t.Fatalf("expected the open refusal, got %v", beginErr)
    }

    if 0 != id || ctx != answeredCtx {
        t.Fatalf("expected nothing to be bound on a refusal, got id=%d", id)
    }
}
