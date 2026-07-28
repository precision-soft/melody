package main

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "os"
    "strings"

    "github.com/uptrace/bun"
)

const mysqlLabel = "mysql and audit"

const mysqlRateLimitKey = "melody-example-v3:rate_limit:"

const (
    mysqlProductProbeName    = "e2e audit probe"
    mysqlProductProbeRenamed = "e2e audit probe renamed"
)

/* both names, because the section renames its probe and the cleanup has to find it under either one */
var mysqlProductProbeNames = []string{mysqlProductProbeName, mysqlProductProbeRenamed}

/* mysqlAuditRow is one entry of the trail as the database holds it. */
type mysqlAuditRow struct {
    Operation string
    Actor     string
    EntityId  string
    Changes   string
}

type mysqlAuditChange struct {
    Field string `json:"field"`
    Old   any    `json:"old"`
    New   any    `json:"new"`
}

/* runMysqlCheck exercises the two properties of the bunorm layer that only a live database can show, and it
exercises them on the tables the application actually uses rather than on tables kept for the purpose.

ENCRYPTED AT REST needs both halves to mean anything: the value the application reads back must be the plaintext
it wrote (so the cipher round-trips) AND the bytes actually sitting in the column must be neither empty nor the
plaintext (so the encryption happened at all). A cipher that silently degraded to a pass-through satisfies the
first half perfectly and loses every secret in the database. The column is the second factor's shared secret —
a value that MUST be stored reversibly, because verifying a code recomputes from it, which is exactly the case
transparent column encryption exists for. The out-of-band half re-reads it through the HARNESS's own connection
instead of trusting what the application reported: the application's own read goes back through the same
EncryptedString type that wrote it, so a broken write and a broken read cancel out and the response still looks
correct.

The AUDIT half asserts the trail of a real catalogue write: an INSERT recording every field and an UPDATE
recording only what moved, with both the old and the new value, attributed to the account that signed in rather
than to an actor a handler put on the context for the occasion. A recorder that logged the operation but lost
the change set produces a trail that satisfies a count assertion and is useless for its only purpose.

The REDACTION half is the other thing a trail must get right: a password change has to be recorded as a change
and must never carry the value. A trail that kept credentials is worse than no trail at all. */
func runMysqlCheck(baseUrl string, redisAddress string) {
    dsn := mysqlDsnOrSkip()
    if "" == dsn {
        return
    }

    if "" == redisAddress {
        skip(
            "%s: REDIS_ADDRESS is cleared, so the write budget an earlier section spent cannot be reset and the audited writes would be refused",
            mysqlLabel,
        )

        return
    }

    database := openMysql(mysqlLabel, dsn)
    defer func() {
        _ = database.Close()
    }()

    assertMysqlEncryptedAtRest(baseUrl, database)

    resetExampleRateLimitCounters(mysqlLabel, redisAddress, mysqlRateLimitKey)

    client := newExampleHttpClient()
    signInExampleHttpAdmin(client, baseUrl)

    assertMysqlProductAuditTrail(client, baseUrl, database)
    assertMysqlPasswordRedactedInTrail(client, baseUrl, database)
}

func mysqlDsnOrSkip() string {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        skip(
            "%s: MYSQL_DSN is not set — neither the ciphertext nor the audit trail was read out of band, so nothing here was checked against the database",
            mysqlLabel,
        )
    }

    return dsn
}

/* assertMysqlEncryptedAtRest enrolls a throwaway second factor, then reads the stored secret the harness's own
way. The enrollment is the section's own rather than borrowed from TWO-FACTOR, which removes the row it created
when it is done; a section that depended on another's leftovers would pass or fail on running order. */
func assertMysqlEncryptedAtRest(baseUrl string, database *bun.DB) {
    user := liveExampleUnique("e2e-encrypted-at-rest")

    client := newLiveExampleClient(baseUrl)
    path := twoFactorEnrollRoute + "?user=" + user

    response := client.call(mysqlLabel, liveExampleRequest{method: "POST", path: path})
    requireLiveExampleStatus(mysqlLabel, path, response, http.StatusOK)

    enrollment := twoFactorEnrollPayload{}
    decodeLiveExamplePayload(mysqlLabel, response, &enrollment)

    if "" == enrollment.Secret {
        fail("%s: the enrollment returned an empty secret, so there is nothing to look for in the column", mysqlLabel)
    }

    defer removeMysqlEnrollment(database, user)

    stored := readMysqlEnrollmentSecret(database, user)

    if 0 == len(stored) {
        fail("%s: the harness's own read of the enrollment for %q found an EMPTY column — the row holds no ciphertext at all", mysqlLabel, user)
    }

    if true == bytes.Equal([]byte(enrollment.Secret), stored) {
        fail(
            "%s: the column holds the shared secret %q verbatim — the cipher degraded to a pass-through and every second factor in the database is forgeable",
            mysqlLabel,
            enrollment.Secret,
        )
    }

    if true == bytes.Contains(stored, []byte(enrollment.Secret)) {
        fail(
            "%s: the harness's own read found the shared secret EMBEDDED in the stored bytes — the value is not encrypted at rest",
            mysqlLabel,
        )
    }

    /* the application must still be able to read what it wrote, or the encryption would have cost the feature
       rather than protected it: a code computed from the enrolled secret has to verify */
    code := assertTwoFactorCodeAccepted(client, user, enrollment.Secret)
    if "" == code {
        fail("%s: the enrolled second factor produced no code", mysqlLabel)
    }

    pass(
        "the second factor's shared secret is %d bytes of ciphertext in %s with no trace of the %d-character secret, and the application still verified a code computed from it",
        len(stored),
        twoFactorTable,
        len(enrollment.Secret),
    )
}

func readMysqlEnrollmentSecret(database *bun.DB, user string) []byte {
    query := fmt.Sprintf("SELECT secret FROM %s WHERE %s = ?", twoFactorTable, twoFactorPrimaryKeyColumn)

    stored := []byte{}
    if scanErr := database.QueryRowContext(context.Background(), query, user).Scan(&stored); nil != scanErr {
        fail("%s: re-read the enrollment for %q from the harness's own connection: %v", mysqlLabel, user, scanErr)
    }

    return stored
}

func removeMysqlEnrollment(database *bun.DB, user string) {
    _, deleteErr := database.
        NewDelete().
        Table(twoFactorTable).
        Where(twoFactorPrimaryKeyColumn+" = ?", user).
        Exec(context.Background())
    if nil != deleteErr {
        fail("%s: remove the throwaway enrollment for %q: %v", mysqlLabel, user, deleteErr)
    }
}

/* assertMysqlProductAuditTrail drives a real catalogue write and pins what the trail recorded. The probe is
created and renamed inside this section and removed at the end, so the expected content is exact. */
func assertMysqlProductAuditTrail(client *http.Client, baseUrl string, database *bun.DB) {
    removeExampleV3ProductProbes(mysqlLabel, database, mysqlProductProbeNames)
    defer removeExampleV3ProductProbes(mysqlLabel, database, mysqlProductProbeNames)

    productId := createMysqlProductProbe(client, baseUrl)
    updateMysqlProductProbe(client, baseUrl, productId)

    trail := readMysqlAuditTrail(database, "product", productId)

    if 2 != len(trail) {
        fail(
            "%s: the trail for %s holds %d entries, wanted exactly the INSERT and the UPDATE the section performed",
            mysqlLabel,
            productId,
            len(trail),
        )
    }

    if "INSERT" != trail[0].Operation {
        fail("%s: the first trail entry is %q, wanted INSERT — the trail is out of order", mysqlLabel, trail[0].Operation)
    }
    if "UPDATE" != trail[1].Operation {
        fail("%s: the second trail entry is %q, wanted UPDATE", mysqlLabel, trail[1].Operation)
    }

    /* the actor is whoever the request authenticated as. An entry attributed to a constant the handler chose
       would answer "who changed this" with the name of the code path rather than of a person */
    adminIdentifier := requireMysqlUserIdentifier(database, exampleHttpAdminUsername)
    for index, entry := range trail {
        if adminIdentifier != entry.Actor {
            fail(
                "%s: trail entry %d records actor %q, wanted the signed-in admin %q — an unattributed entry cannot answer who acted",
                mysqlLabel,
                index,
                entry.Actor,
                adminIdentifier,
            )
        }
    }

    insertChanges := decodeMysqlChanges(trail[0])
    for _, field := range []string{"id", "name", "price", "stock"} {
        if false == mysqlFieldRecorded(insertChanges, field) {
            fail("%s: the INSERT entry records no change for %q (recorded: %v)", mysqlLabel, field, mysqlChangedFields(insertChanges))
        }
    }

    /* updated_at moves on every write and is globally ignored, so an entry that recorded it would mean the
       ignore list is not being applied at all */
    if true == mysqlFieldRecorded(insertChanges, "updated_at") {
        fail("%s: the INSERT entry records updated_at, which the registry ignores — the ignore list is not applied", mysqlLabel)
    }

    /* the UPDATE must record ONLY what moved and must carry the previous value: an entry that repeated every
       field, or that dropped the old value, makes the trail unable to answer "what changed" — its only question */
    updateChanges := decodeMysqlChanges(trail[1])
    if 2 != len(updateChanges) {
        fail(
            "%s: the UPDATE entry records %d changed field(s) %v, wanted exactly name and price",
            mysqlLabel,
            len(updateChanges),
            mysqlChangedFields(updateChanges),
        )
    }

    for _, change := range updateChanges {
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

    nameChange := mysqlChangeOf(updateChanges, "name")
    if mysqlProductProbeName != fmt.Sprintf("%v", nameChange.Old) {
        fail("%s: the UPDATE entry records the old name as %v, wanted %q", mysqlLabel, nameChange.Old, mysqlProductProbeName)
    }
    if mysqlProductProbeRenamed != fmt.Sprintf("%v", nameChange.New) {
        fail("%s: the UPDATE entry records the new name as %v, wanted %q", mysqlLabel, nameChange.New, mysqlProductProbeRenamed)
    }

    pass(
        "the trail of %s holds exactly INSERT then UPDATE, attributed to %s, with the change set the update actually made",
        productId,
        adminIdentifier,
    )
}

/* assertMysqlPasswordRedactedInTrail changes the password of an account the section creates for the purpose. It
is never a seeded account: the seeded credentials are what every other section signs in with, and a run that left
one of them changed would take the whole harness down with it on the next execution. */
func assertMysqlPasswordRedactedInTrail(client *http.Client, baseUrl string, database *bun.DB) {
    username := liveExampleUnique("e2e-redaction")

    userId := createMysqlUserProbe(client, baseUrl, username)
    defer removeMysqlUserProbe(client, baseUrl, database, userId)

    updateMysqlUserProbePassword(client, baseUrl, userId, username)

    trail := readMysqlAuditTrail(database, "user", userId)

    if 2 != len(trail) {
        fail(
            "%s: the trail for the probe account holds %d entries, wanted the INSERT and the UPDATE the section performed",
            mysqlLabel,
            len(trail),
        )
    }

    for index, entry := range trail {
        changes := decodeMysqlChanges(entry)

        if false == mysqlFieldRecorded(changes, "password") {
            fail(
                "%s: trail entry %d records no change for the password field — a credential change that leaves no trace cannot be reviewed",
                mysqlLabel,
                index,
            )
        }

        change := mysqlChangeOf(changes, "password")

        for _, value := range []any{change.Old, change.New} {
            if nil == value {
                continue
            }

            rendered := fmt.Sprintf("%v", value)
            if "<redacted>" != rendered {
                fail(
                    "%s: trail entry %d carries the password as %q rather than <redacted> — the trail is a history of credentials",
                    mysqlLabel,
                    index,
                    rendered,
                )
            }
        }
    }

    pass("the password change is recorded as a change with both values masked, so the trail answers what happened without keeping the credential")
}

func createMysqlProductProbe(client *http.Client, baseUrl string) string {
    body := `{"name":"` + mysqlProductProbeName + `","description":"probe","categoryId":"cat-1","price":9,"currencyId":"cur-eur","stock":2}`

    payload := requireMysqlWrite(client, "POST", baseUrl, "/products/api/create/", body, "create the audit probe")

    created := exampleProduct{}
    decodeExampleData(mysqlLabel, "the audit probe", payload, &created)

    if "" == created.Id {
        fail("%s: the audit probe was accepted without an identifier: %s", mysqlLabel, exampleTruncate(payload))
    }

    return created.Id
}

func updateMysqlProductProbe(client *http.Client, baseUrl string, productId string) {
    body := `{"name":"` + mysqlProductProbeRenamed + `","description":"probe","categoryId":"cat-1","price":11,"currencyId":"cur-eur","stock":2}`

    requireMysqlWrite(client, "PUT", baseUrl, "/products/api/update/"+productId+"/", body, "rename the audit probe")
}

func createMysqlUserProbe(client *http.Client, baseUrl string, username string) string {
    body := `{"username":"` + username + `","password":"first-password","roles":["ROLE_USER"]}`

    payload := requireMysqlWrite(client, "POST", baseUrl, "/users/api/create/", body, "create the redaction probe account")

    created := struct {
        Id string `json:"id"`
    }{}
    decodeExampleData(mysqlLabel, "the redaction probe account", payload, &created)

    if "" == created.Id {
        fail("%s: the probe account was accepted without an identifier: %s", mysqlLabel, exampleTruncate(payload))
    }

    return created.Id
}

func updateMysqlUserProbePassword(client *http.Client, baseUrl string, userId string, username string) {
    body := `{"username":"` + username + `","password":"second-password","roles":["ROLE_USER"]}`

    requireMysqlWrite(client, "PUT", baseUrl, "/users/api/update/"+userId+"/", body, "change the probe account's password")
}

func removeMysqlUserProbe(client *http.Client, baseUrl string, database *bun.DB, userId string) {
    requireMysqlWrite(client, "DELETE", baseUrl, "/users/api/delete/"+userId+"/", "", "remove the redaction probe account")

    removeExampleV3AuditTrail(mysqlLabel, database, "user", userId)
}

/* requireMysqlWrite performs one write and reports the body. A refusal is named for what it is, because a write
that never reached the nomenclature would leave every assertion below measuring an empty trail. */
func requireMysqlWrite(client *http.Client, method string, baseUrl string, path string, body string, what string) string {
    var reader io.Reader
    if "" != body {
        reader = strings.NewReader(body)
    }

    request, requestErr := http.NewRequest(method, strings.TrimRight(baseUrl, "/")+path, reader)
    if nil != requestErr {
        fail("%s: build the request to %s: %v", mysqlLabel, what, requestErr)
    }

    if "" != body {
        request.Header.Set("Content-Type", "application/json")
    }
    request.Header.Set("Accept", "application/json")

    response, responseErr := client.Do(request)
    if nil != responseErr {
        fail("%s: %s: %v", mysqlLabel, what, responseErr)
    }
    defer response.Body.Close()

    payload, readErr := io.ReadAll(response.Body)
    if nil != readErr {
        fail("%s: read the response to %s: %v", mysqlLabel, what, readErr)
    }

    if http.StatusUnauthorized == response.StatusCode || http.StatusForbidden == response.StatusCode {
        fail("%s: %s was refused with %d — the section reached the firewall, not the nomenclature", mysqlLabel, what, response.StatusCode)
    }

    if http.StatusTooManyRequests == response.StatusCode {
        fail("%s: %s was refused with 429 — the write budget was not reset before this section", mysqlLabel, what)
    }

    if http.StatusOK != response.StatusCode && http.StatusCreated != response.StatusCode {
        fail("%s: %s was refused with %d: %s", mysqlLabel, what, response.StatusCode, exampleTruncate(string(payload)))
    }

    return string(payload)
}

func readMysqlAuditTrail(database *bun.DB, entity string, entityId string) []mysqlAuditRow {
    rows := make([]mysqlAuditRow, 0, 2)

    selectErr := database.
        NewSelect().
        ColumnExpr("operation AS operation").
        ColumnExpr("actor AS actor").
        ColumnExpr("entity_id AS entity_id").
        ColumnExpr("changes AS changes").
        Table(exampleV3AuditTable).
        Where("entity = ?", entity).
        Where("entity_id = ?", entityId).
        Order("id ASC").
        Scan(context.Background(), &rows)
    if nil != selectErr {
        fail("%s: read the trail of %s %s out of band: %v", mysqlLabel, entity, entityId, selectErr)
    }

    return rows
}

func requireMysqlUserIdentifier(database *bun.DB, username string) string {
    identifiers := make([]string, 0, 1)

    selectErr := database.
        NewSelect().
        ColumnExpr("id").
        Table(exampleV3UserTable).
        Where("username = ?", username).
        Scan(context.Background(), &identifiers)
    if nil != selectErr {
        fail("%s: look up %q in %s: %v", mysqlLabel, username, exampleV3UserTable, selectErr)
    }

    if 1 != len(identifiers) {
        fail("%s: the directory holds %d accounts named %q", mysqlLabel, len(identifiers), username)
    }

    return identifiers[0]
}

func decodeMysqlChanges(row mysqlAuditRow) []mysqlAuditChange {
    changes := []mysqlAuditChange{}

    if decodeErr := json.Unmarshal([]byte(row.Changes), &changes); nil != decodeErr {
        fail("%s: the %s entry for %s holds an undecodable change set (%v): %s", mysqlLabel, row.Operation, row.EntityId, decodeErr, exampleTruncate(row.Changes))
    }

    return changes
}

func mysqlChangedFields(changes []mysqlAuditChange) []string {
    fields := []string{}

    for _, change := range changes {
        fields = append(fields, change.Field)
    }

    return fields
}

func mysqlFieldRecorded(changes []mysqlAuditChange, wanted string) bool {
    for _, change := range changes {
        if wanted == change.Field {
            return true
        }
    }

    return false
}

func mysqlChangeOf(changes []mysqlAuditChange, field string) mysqlAuditChange {
    for _, change := range changes {
        if field == change.Field {
            return change
        }
    }

    fail("%s: the change set records nothing for %q (recorded: %v)", mysqlLabel, field, mysqlChangedFields(changes))

    return mysqlAuditChange{}
}
