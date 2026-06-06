package handler

import (
    "encoding/json"
    nethttp "net/http"
    "time"

    melodyaudit "github.com/precision-soft/melody/integrations/bunorm/v3/audit"
    melodyencrypt "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
    bun "github.com/uptrace/bun"
)

func CacheDemoHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        cacheInstance := melodycache.CacheMustFromContainer(runtimeInstance.Container())

        key := "demo:cache:key"
        value := "cached-value"

        if setErr := cacheInstance.Set(key, value, 60*time.Second); nil != setErr {
            return nil, setErr
        }

        stored, found, getErr := cacheInstance.Get(key)
        if nil != getErr {
            return nil, getErr
        }

        return melodyhttp.JsonResponse(nethttp.StatusOK, map[string]any{
            "key":    key,
            "stored": stored,
            "found":  found,
        })
    }
}

func EncryptDemoHandler(cipher melodyencrypt.Cipher) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        plaintext := "sensitive-value"

        ciphertext, encryptErr := cipher.Encrypt(plaintext)
        if nil != encryptErr {
            return nil, encryptErr
        }

        decrypted, decryptErr := cipher.Decrypt(ciphertext)
        if nil != decryptErr {
            return nil, decryptErr
        }

        return melodyhttp.JsonResponse(nethttp.StatusOK, map[string]any{
            "plaintext":  plaintext,
            "ciphertext": ciphertext,
            "decrypted":  decrypted,
            "roundTrip":  plaintext == decrypted,
        })
    }
}

func RedisTokenDemoHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        store := melodyrueidis.TokenStoreMustFromResolver(runtimeInstance.Container())

        token := "demo-redis-token"
        store.Put(token, melodysecuritycontract.Claims{
            UserIdentifier: "redis-demo-user",
            Roles:          []string{"ROLE_USER"},
        })

        claims, foundBeforeRevoke, lookupErr := store.Lookup(runtimeInstance, token)
        if nil != lookupErr {
            return nil, lookupErr
        }

        store.Delete(token)

        _, foundAfterRevoke, revokeLookupErr := store.Lookup(runtimeInstance, token)
        if nil != revokeLookupErr {
            return nil, revokeLookupErr
        }

        return melodyhttp.JsonResponse(nethttp.StatusOK, map[string]any{
            "user":              claims.UserIdentifier,
            "foundBeforeRevoke": foundBeforeRevoke,
            "foundAfterRevoke":  foundAfterRevoke,
        })
    }
}

type databaseDemoSecret struct {
    bun.BaseModel `bun:"table:melody_example_secrets"`

    Id     int64                         `bun:"id,pk,autoincrement"`
    Label  string                        `bun:"label,notnull"`
    Secret melodyencrypt.EncryptedString `bun:"secret,type:varbinary(512),notnull"`
}

func DatabaseDemoHandler(database *bun.DB) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        ctx := request.HttpRequest().Context()

        if _, createErr := database.NewCreateTable().Model((*databaseDemoSecret)(nil)).IfNotExists().Exec(ctx); nil != createErr {
            return nil, createErr
        }

        row := &databaseDemoSecret{
            Label:  "demo",
            Secret: melodyencrypt.EncryptedString("top-secret-value"),
        }

        if _, insertErr := database.NewInsert().Model(row).Exec(ctx); nil != insertErr {
            return nil, insertErr
        }

        loaded := new(databaseDemoSecret)
        if selectErr := database.NewSelect().Model(loaded).Where("id = ?", row.Id).Scan(ctx); nil != selectErr {
            return nil, selectErr
        }

        var storedCiphertext string
        if rawErr := database.NewSelect().ColumnExpr("secret").Table("melody_example_secrets").Where("id = ?", row.Id).Scan(ctx, &storedCiphertext); nil != rawErr {
            return nil, rawErr
        }

        return melodyhttp.JsonResponse(nethttp.StatusOK, map[string]any{
            "id":               row.Id,
            "decrypted":        string(loaded.Secret),
            "storedCiphertext": storedCiphertext,
        })
    }
}

type auditDemoProduct struct {
    bun.BaseModel `bun:"table:melody_example_products"`

    Id    int64  `bun:"id,pk"`
    Name  string `bun:"name,notnull"`
    Price int64  `bun:"price,notnull"`
}

func AuditDemoHandler(database *bun.DB) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        ctx := melodyaudit.WithActor(request.HttpRequest().Context(), "demo-actor")

        recorder := melodyaudit.NewRecorder(database, melodyaudit.DefaultTable)
        if schemaErr := recorder.Registry().EnsureSchema(ctx, database); nil != schemaErr {
            return nil, schemaErr
        }

        tracker := melodyaudit.NewTracker(database, recorder)

        if _, createErr := database.NewCreateTable().Model((*auditDemoProduct)(nil)).IfNotExists().Exec(ctx); nil != createErr {
            return nil, createErr
        }

        if _, clearRowErr := database.NewDelete().Model((*auditDemoProduct)(nil)).Where("id = ?", 1).Exec(ctx); nil != clearRowErr {
            return nil, clearRowErr
        }

        if _, clearTrailErr := database.NewDelete().Model((*melodyaudit.Entry)(nil)).Where("entity = ? AND entity_id = ?", "product", "1").Exec(ctx); nil != clearTrailErr {
            return nil, clearTrailErr
        }

        product := &auditDemoProduct{Id: 1, Name: "Widget", Price: 1000}
        if insertErr := tracker.Insert(ctx, "product", "1", product); nil != insertErr {
            return nil, insertErr
        }

        product.Name = "Widget Pro"
        product.Price = 1500
        if updateErr := tracker.Update(ctx, "product", "1", product); nil != updateErr {
            return nil, updateErr
        }

        var entries []melodyaudit.Entry
        if selectErr := database.NewSelect().Model(&entries).Where("entity = ? AND entity_id = ?", "product", "1").Order("id ASC").Scan(ctx); nil != selectErr {
            return nil, selectErr
        }

        trail := make([]map[string]any, 0, len(entries))
        for _, entry := range entries {
            var changes any
            json.Unmarshal([]byte(entry.Changes), &changes)

            trail = append(trail, map[string]any{
                "operation": entry.Operation,
                "actor":     entry.Actor,
                "changes":   changes,
            })
        }

        return melodyhttp.JsonResponse(nethttp.StatusOK, map[string]any{
            "entity": "product:1",
            "trail":  trail,
        })
    }
}
