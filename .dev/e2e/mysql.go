package main

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "os"
)

const mysqlLabel = "mysql and audit"

const (
    mysqlDemoRoute  = "/database/demo"
    mysqlAuditRoute = "/database/audit/demo"
)

/* mysqlSecretTable is the demo table the /database/demo handler writes an EncryptedString into, and
mysqlSecretColumn is the column that must hold ciphertext rather than the value the caller passed
(handler/integration_demo_handler.go). */
const (
    mysqlSecretTable  = "melody_example_secrets"
    mysqlSecretColumn = "secret"
)

/* mysqlDemoPlaintext is the value the handler stores; it is hardcoded there, so the harness knows what the
ciphertext must NOT be equal to. */
const mysqlDemoPlaintext = "top-secret-value"

/* both /database routes answer with a plain http.JsonResponse rather than the presenter's success/payload/errors
envelope, so these decode straight into their own shapes. */
type mysqlDemoPayload struct {
    Id               int64  `json:"id"`
    Decrypted        string `json:"decrypted"`
    StoredCiphertext string `json:"storedCiphertext"`
}

type mysqlAuditPayload struct {
    Entity string `json:"entity"`
    Trail  []struct {
        Operation string `json:"operation"`
        Actor     string `json:"actor"`
        Changes   []struct {
            Field    string `json:"field"`
            Old      any    `json:"old"`
            New      any    `json:"new"`
        } `json:"changes"`
    } `json:"trail"`
}

/* runMysqlCheck exercises the two properties of the bunorm layer that only a live database can show.

The one that matters is ENCRYPTED AT REST, and it needs both halves to mean anything: the value the application
reads back must be the plaintext it wrote (so the cipher round-trips) AND the bytes actually sitting in the column
must be neither empty nor the plaintext (so the encryption happened at all). A cipher that silently degraded to a
pass-through satisfies the first half perfectly and loses every secret in the database.

The out-of-band half re-reads that column through the HARNESS's own connection instead of trusting the value the
application reported. That is what makes this a MySQL section rather than a json-shape assertion: the application's
own read passes through the same EncryptedString type that wrote it, so a broken write and a broken read cancel out
and the response still looks correct.

The audit half asserts the trail holds EXACTLY the expected entries, in order, with the right change set — an
INSERT recording every field and an UPDATE recording only what moved, with both the old and the new value. A
recorder that logged the operation but lost the change set produces a trail that satisfies a count assertion and is
useless for its only purpose. */
func runMysqlCheck(baseUrl string) {
    client := newLiveExampleClient(baseUrl)

    payload := assertMysqlEncryptedAtRest(client)
    assertMysqlCiphertextOutOfBand(payload)
    assertMysqlAuditTrail(client)
}

func assertMysqlEncryptedAtRest(client *liveExampleClient) mysqlDemoPayload {
    response := client.get(mysqlLabel, mysqlDemoRoute)
    requireLiveExampleStatus(mysqlLabel, mysqlDemoRoute, response, http.StatusOK)

    payload := mysqlDemoPayload{}
    if decodeErr := json.Unmarshal(response.body, &payload); nil != decodeErr {
        fail(
            "%s: %s answered 200 with an undecodable body (%v): %s",
            mysqlLabel,
            mysqlDemoRoute,
            decodeErr,
            exampleTruncate(response.bodyText()),
        )
    }

    if 0 >= payload.Id {
        fail("%s: %s reported id %d, so no row was inserted", mysqlLabel, mysqlDemoRoute, payload.Id)
    }
    if mysqlDemoPlaintext != payload.Decrypted {
        fail(
            "%s: the value read back through the application decrypted to %q, wanted %q",
            mysqlLabel,
            payload.Decrypted,
            mysqlDemoPlaintext,
        )
    }
    if "" == payload.StoredCiphertext {
        fail("%s: the stored column is EMPTY — the row holds no ciphertext at all", mysqlLabel)
    }
    if mysqlDemoPlaintext == payload.StoredCiphertext {
        fail(
            "%s: the stored column holds the plaintext %q verbatim — the cipher degraded to a pass-through and every secret in the database is readable",
            mysqlLabel,
            payload.StoredCiphertext,
        )
    }

    pass(
        "the round trip decrypted to %q while the stored column holds %d bytes of something else (row id %d)",
        payload.Decrypted,
        len(payload.StoredCiphertext),
        payload.Id,
    )

    return payload
}

/* assertMysqlCiphertextOutOfBand re-reads the column the harness's own way. The column is varbinary and the
ciphertext carries NUL bytes, so it is read as raw bytes; comparing against the plaintext as bytes is what catches
a pass-through that the application's own read would have hidden. */
func assertMysqlCiphertextOutOfBand(payload mysqlDemoPayload) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        skip(
            "%s: MYSQL_DSN is not set — the ciphertext was NOT re-read out of band, so only the application's own view of the column was checked",
            mysqlLabel,
        )

        return
    }

    database := openMysql(mysqlLabel, dsn)
    defer func() {
        _ = database.Close()
    }()

    query := fmt.Sprintf("SELECT %s FROM %s WHERE id = ?", mysqlSecretColumn, mysqlSecretTable)

    stored := []byte{}
    if scanErr := database.QueryRowContext(context.Background(), query, payload.Id).Scan(&stored); nil != scanErr {
        fail("%s: re-read row %d of %s from the harness's own connection: %v", mysqlLabel, payload.Id, mysqlSecretTable, scanErr)
    }

    if 0 == len(stored) {
        fail("%s: the harness's own read of row %d found an empty column", mysqlLabel, payload.Id)
    }
    if true == bytes.Equal([]byte(mysqlDemoPlaintext), stored) {
        fail(
            "%s: the harness's own read of row %d found the plaintext %q in the column — the application reported ciphertext it did not store",
            mysqlLabel,
            payload.Id,
            mysqlDemoPlaintext,
        )
    }
    if true == bytes.Contains(stored, []byte(mysqlDemoPlaintext)) {
        fail(
            "%s: the harness's own read of row %d found the plaintext EMBEDDED in the stored bytes — the value is not encrypted at rest",
            mysqlLabel,
            payload.Id,
        )
    }

    /* the two views have to agree as well: a mismatch means the application reported a ciphertext other than the
       one it committed, which is the same class of defect seen from the other side */
    if false == bytes.Equal([]byte(payload.StoredCiphertext), stored) {
        fail(
            "%s: the application reported %d ciphertext bytes for row %d while the harness's own read found %d — the reported value is not what was committed",
            mysqlLabel,
            len(payload.StoredCiphertext),
            payload.Id,
            len(stored),
        )
    }

    pass(
        "the harness's own connection re-read row %d of %s and found %d bytes of ciphertext with no trace of the plaintext",
        payload.Id,
        mysqlSecretTable,
        len(stored),
    )
}

/* assertMysqlAuditTrail pins the trail exactly. The handler clears the row and its trail before writing, so the
expected content is deterministic: an INSERT carrying all three fields with no old value, then an UPDATE carrying
only the two that moved, each with both the old and the new value. */
func assertMysqlAuditTrail(client *liveExampleClient) {
    response := client.get(mysqlLabel, mysqlAuditRoute)
    requireLiveExampleStatus(mysqlLabel, mysqlAuditRoute, response, http.StatusOK)

    payload := mysqlAuditPayload{}
    if decodeErr := json.Unmarshal(response.body, &payload); nil != decodeErr {
        fail(
            "%s: %s answered 200 with an undecodable body (%v): %s",
            mysqlLabel,
            mysqlAuditRoute,
            decodeErr,
            exampleTruncate(response.bodyText()),
        )
    }

    if 2 != len(payload.Trail) {
        fail(
            "%s: the audit trail holds %d entries, wanted exactly the INSERT and the UPDATE the handler performs: %s",
            mysqlLabel,
            len(payload.Trail),
            exampleTruncate(response.bodyText()),
        )
    }

    if "INSERT" != payload.Trail[0].Operation {
        fail("%s: the first trail entry is %q, wanted INSERT — the trail is out of order", mysqlLabel, payload.Trail[0].Operation)
    }
    if "UPDATE" != payload.Trail[1].Operation {
        fail("%s: the second trail entry is %q, wanted UPDATE", mysqlLabel, payload.Trail[1].Operation)
    }

    for index, entry := range payload.Trail {
        if "demo-actor" != entry.Actor {
            fail(
                "%s: trail entry %d records actor %q, wanted the actor the request put on the context (%q) — an unattributed entry cannot answer who acted",
                mysqlLabel,
                index,
                entry.Actor,
                "demo-actor",
            )
        }
    }

    insertFields := mysqlChangedFields(payload, 0)
    for _, field := range []string{"id", "name", "price"} {
        if false == mysqlFieldRecorded(insertFields, field) {
            fail("%s: the INSERT entry records no change for %q (recorded: %v)", mysqlLabel, field, insertFields)
        }
    }

    /* the UPDATE must record ONLY what moved and must carry the previous value: an entry that repeated every field,
       or that dropped the old value, makes the trail unable to answer "what changed" — its only question */
    updateFields := mysqlChangedFields(payload, 1)
    if 2 != len(updateFields) {
        fail(
            "%s: the UPDATE entry records %d changed field(s) %v, wanted exactly name and price",
            mysqlLabel,
            len(updateFields),
            updateFields,
        )
    }
    for _, field := range []string{"name", "price"} {
        if false == mysqlFieldRecorded(updateFields, field) {
            fail("%s: the UPDATE entry records no change for %q (recorded: %v)", mysqlLabel, field, updateFields)
        }
    }

    for _, change := range payload.Trail[1].Changes {
        if nil == change.Old {
            fail(
                "%s: the UPDATE entry records %q with no previous value, so the trail cannot say what it changed FROM",
                mysqlLabel,
                change.Field,
            )
        }
        if nil == change.New {
            fail("%s: the UPDATE entry records %q with no new value", mysqlLabel, change.Field)
        }
    }

    if "Widget" != fmt.Sprintf("%v", mysqlChangeOf(payload, 1, "name").Old) {
        fail("%s: the UPDATE entry records the old name as %v, wanted \"Widget\"", mysqlLabel, mysqlChangeOf(payload, 1, "name").Old)
    }
    if "Widget Pro" != fmt.Sprintf("%v", mysqlChangeOf(payload, 1, "name").New) {
        fail("%s: the UPDATE entry records the new name as %v, wanted \"Widget Pro\"", mysqlLabel, mysqlChangeOf(payload, 1, "name").New)
    }

    pass("the audit trail holds exactly INSERT then UPDATE for %s, attributed to demo-actor, with the change set the update actually made", payload.Entity)
}

type mysqlChange struct {
    Field string
    Old   any
    New   any
}

func mysqlChangedFields(payload mysqlAuditPayload, index int) []string {
    fields := []string{}

    for _, change := range payload.Trail[index].Changes {
        fields = append(fields, change.Field)
    }

    return fields
}

func mysqlFieldRecorded(fields []string, wanted string) bool {
    for _, field := range fields {
        if wanted == field {
            return true
        }
    }

    return false
}

func mysqlChangeOf(payload mysqlAuditPayload, index int, field string) mysqlChange {
    for _, change := range payload.Trail[index].Changes {
        if field == change.Field {
            return mysqlChange{Field: change.Field, Old: change.Old, New: change.New}
        }
    }

    fail("%s: trail entry %d records no change for %q", mysqlLabel, index, field)

    return mysqlChange{}
}
