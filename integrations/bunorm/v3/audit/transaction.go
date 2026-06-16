package audit

import (
    "context"
    "encoding/json"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/uptrace/bun"
)

const DefaultTransactionTable = "melody_audit_transaction"

type Transaction struct {
    bun.BaseModel `bun:"table:melody_audit_transaction,alias:melody_audit_transaction"`

    Id        int64     `bun:"id,pk,autoincrement"`
    Actor     string    `bun:"actor"`
    Extras    string    `bun:"extras,type:text"`
    CreatedAt time.Time `bun:"created_at,notnull"`
}

type transactionContextKey struct{}

func transactionIdFromContext(ctx context.Context) int64 {
    id, _ := ctx.Value(transactionContextKey{}).(int64)

    return id
}

func BeginTransaction(ctx context.Context, database *bun.DB, actor string, extras map[string]any) (context.Context, int64, error) {
    encodedExtras := ""
    if 0 != len(extras) {
        payload, marshalErr := json.Marshal(extras)
        if nil != marshalErr {
            return ctx, 0, exception.NewError("could not encode audit transaction extras", nil, marshalErr)
        }
        encodedExtras = string(payload)
    }

    transaction := Transaction{
        Actor:     actor,
        Extras:    encodedExtras,
        CreatedAt: time.Now(),
    }

    if _, insertErr := databaseFromContext(ctx, database).NewInsert().Model(&transaction).Exec(ctx); nil != insertErr {
        return ctx, 0, exception.NewError("could not open the audit transaction", nil, insertErr)
    }

    return context.WithValue(ctx, transactionContextKey{}, transaction.Id), transaction.Id, nil
}
