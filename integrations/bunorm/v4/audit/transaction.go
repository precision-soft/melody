package audit

import (
    "context"
    "encoding/json"
    "time"

    "github.com/precision-soft/melody/v4/exception"
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

/* TransactionOptions is what one audit transaction records beside its id. */
type TransactionOptions struct {
    /* Actor is who the transaction is recorded for; left out, the row's actor is empty. */
    Actor string
    /* Extras are encoded as JSON into the row; nil or empty records none. */
    Extras map[string]any
}

/* BeginTransaction answers the context an audited operation records under, so it takes and answers a context, never a runtime. */
func BeginTransaction(ctx context.Context, database *bun.DB, options TransactionOptions) (context.Context, int64, error) {
    encodedExtras := ""
    if 0 != len(options.Extras) {
        payload, marshalErr := json.Marshal(options.Extras)
        if nil != marshalErr {
            return ctx, 0, exception.NewError("could not encode audit transaction extras", nil, marshalErr)
        }
        encodedExtras = string(payload)
    }

    transaction := Transaction{
        Actor:     options.Actor,
        Extras:    encodedExtras,
        CreatedAt: time.Now(),
    }

    if _, insertErr := databaseFromContext(ctx, database).NewInsert().Model(&transaction).Exec(ctx); nil != insertErr {
        return ctx, 0, exception.NewError("could not open the audit transaction", nil, insertErr)
    }

    return context.WithValue(ctx, transactionContextKey{}, transaction.Id), transaction.Id, nil
}
