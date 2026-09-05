package main

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"

    "github.com/uptrace/bun"
)

/* The seeded editor account. The nomenclature's write endpoints sit behind ROLE_EDITOR, so the section's own client — which holds ROLE_USER and is what makes the admin route answer 403 — cannot drive them. */
const (
    exampleEditorUsername = "editor"
    exampleEditorPassword = "editor"
)

const (
    exampleProductListRoute   = "/products/api/read/"
    exampleProductCreateRoute = "/products/api/create/"
    exampleCatalogReportRoute = "/catalog/report/"

    /* the listing cache key, as the application spells it before its per-major prefix */
    exampleProductListCacheKey = "example-product-list"

    /* the write budget config/redis.go declares. One more request than this must be refused. */
    exampleWriteAllowance = 30
)

type exampleEnvelope struct {
    Success bool            `json:"success"`
    Payload json.RawMessage `json:"payload"`
}

type exampleProduct struct {
    Id    string  `json:"id"`
    Name  string  `json:"name"`
    Price float64 `json:"price"`
    Stock int64   `json:"stock"`
}

type exampleJournalEntry struct {
    Id        int64  `json:"id"`
    Actor     string `json:"actor"`
    Action    string `json:"action"`
    Subject   string `json:"subject"`
    SubjectId string `json:"subjectId"`
}

type exampleCatalogReport struct {
    RecordedAt string                `json:"recordedAt"`
    Payload    string                `json:"payload"`
    FromCache  bool                  `json:"fromCache"`
    Journal    []exampleJournalEntry `json:"journal"`
}

/* exampleCachePrefix and the table names carry the major, because the three example applications share one redis and one mysql in development. The application declares the same shape in config/redis.go and in its repository row tags, and a mismatch here would read somebody else's state. */
func exampleCachePrefix(major exampleMajor) string {
    return "melody-example-" + major.label + ":cache:"
}

func exampleMajorRateLimitPrefix(major exampleMajor) string {
    return "melody-example-" + major.label + ":rate_limit:"
}

func exampleProductTable(major exampleMajor) string {
    return "melody_example_" + major.label + "_product"
}

func exampleJournalTable(major exampleMajor) string {
    return "melody_example_" + major.label + "_catalog_journal"
}

/* exampleJournalDsn answers the DSN of the database one major keeps its journal in, together with the environment variable that carries it — the name is what a skip message must say when the value is cleared. The v1 example runs its journal on postgres beside a mysql catalogue; the later majors keep both in mysql. */
func exampleJournalDsn(major exampleMajor, mysqlDsn string, postgresDsn string) (string, string) {
    if true == major.journalOnPostgres {
        return postgresDsn, "POSTGRES_DSN"
    }

    return mysqlDsn, "MYSQL_DSN"
}

/* openExampleJournal opens the journal database through the door its engine requires: the postgres handle is a DSN connector, the mysql one goes through the framework's own provider like every other mysql read. */
func openExampleJournal(major exampleMajor, journalDsn string) *bun.DB {
    if true == major.journalOnPostgres {
        return openPostgres(journalDsn)
    }

    return openMysql(major.label, journalDsn)
}

/* decodeExampleData pulls the payload out of the example's response envelope. The presenter wraps every api answer, so a decode straight into the payload type would silently yield a zero value and turn a broken assertion into a passing one. */
func decodeExampleData(label string, route string, body string, target any) {
    envelope := exampleEnvelope{}

    decodeErr := json.Unmarshal([]byte(body), &envelope)
    if nil != decodeErr {
        fail("[%s] %s: the response is not the api envelope (%v): %s", label, route, decodeErr, body)
    }

    if false == envelope.Success {
        fail("[%s] %s: the api envelope reports a failure: %s", label, route, body)
    }

    if 0 == len(envelope.Payload) {
        fail("[%s] %s: the api envelope carried no payload: %s", label, route, body)
    }

    decodeErr = json.Unmarshal(envelope.Payload, target)
    if nil != decodeErr {
        fail("[%s] %s: the payload does not decode (%v): %s", label, route, decodeErr, body)
    }
}

/* runExampleIntegrationAssertions drives the live backends of one example application through the nomenclature itself rather than through routes that exist to be driven: the catalogue is kept in mysql, its listing is cached in redis and dropped when a product changes, every write leaves a journal entry, and the writes are rate limited.

   Every check is made twice over: once through the application, and once out of band through the harness's own connection to the same backend. The application's own write and read pass through one code path, so a broken pair would cancel out and report success; the out-of-band half is what makes the claim about the backend rather than about the handler. */
func runExampleIntegrationAssertions(
    major exampleMajor,
    client *exampleClient,
    application *exampleApplication,
    redisAddress string,
    mysqlDsn string,
    postgresDsn string,
) {
    editor := exampleEditorClient(major, application)

    assertExampleCatalogReport(major, client)
    createdProductId := assertExampleProductPersisted(major, editor, application, mysqlDsn)
    assertExampleListingCached(major, editor, application, redisAddress, mysqlDsn, postgresDsn)
    assertExampleJournalRecordedTheWrite(major, editor, createdProductId, mysqlDsn, postgresDsn)
    assertExampleWritesAreRateLimited(major, editor, redisAddress)

    removeExampleProbeRows(major, createdProductId, mysqlDsn, postgresDsn)
}

/* exampleEditorClient signs in a second client under the account that may change the nomenclature. The section's own client stays as it is: its ROLE_USER identity is what several assertions above are about. */
func exampleEditorClient(major exampleMajor, application *exampleApplication) *exampleClient {
    editor := newExampleClient(major)

    response := editor.call(
        "POST",
        exampleLoginRoute,
        "application/json",
        "application/x-www-form-urlencoded",
        "username="+exampleEditorUsername+"&password="+exampleEditorPassword,
    )
    if http.StatusOK != response.statusCode {
        fail(
            "[%s] the seeded editor could not sign in (%d), so the nomenclature's writes cannot be driven:\n%s\n%s",
            major.label, response.statusCode, response.body, application.logTail(20),
        )
    }

    return editor
}

/* assertExampleCatalogReport covers the reading that needs no backend at all: it is the one assertion that must hold on every machine, so it is also the proof that the catalogue routes are wired. */
func assertExampleCatalogReport(major exampleMajor, client *exampleClient) {
    response := client.call("GET", exampleCatalogReportRoute, "application/json", "", "")
    if http.StatusOK != response.statusCode {
        fail("[%s] %s: expected 200, got %d: %s", major.label, exampleCatalogReportRoute, response.statusCode, response.body)
    }

    report := exampleCatalogReport{}
    decodeExampleData(major.label, exampleCatalogReportRoute, response.body, &report)

    if "" == report.RecordedAt {
        fail("[%s] %s: the report carried no clock stamp: %s", major.label, exampleCatalogReportRoute, response.body)
    }

    if _, parseErr := time.Parse(time.RFC3339, report.RecordedAt); nil != parseErr {
        fail("[%s] %s: the clock stamp is not a timestamp (%v): %q", major.label, exampleCatalogReportRoute, parseErr, report.RecordedAt)
    }

    /* the reading counts the catalogue it actually read, so a report that stopped looking would say products=0 */
    if false == exampleReportCountsAtLeast(report.Payload, "products=", 1) {
        fail("[%s] %s: the reading reports an empty catalogue: %q", major.label, exampleCatalogReportRoute, report.Payload)
    }

    pass("[%s] the clock-stamped catalogue reading answered with %q and counted the seeded catalogue", major.label, report.RecordedAt)
}

func exampleReportCountsAtLeast(payload string, field string, wanted int) bool {
    value := 0

    _, scanErr := fmt.Sscanf(exampleReportField(payload, field), "%d", &value)
    if nil != scanErr {
        return false
    }

    return wanted <= value
}

func exampleReportField(payload string, field string) string {
    for _, part := range splitExampleReportFields(payload) {
        if len(part) > len(field) && part[:len(field)] == field {
            return part[len(field):]
        }
    }

    return ""
}

func splitExampleReportFields(payload string) []string {
    parts := make([]string, 0, 3)
    current := ""

    for _, character := range payload {
        if ' ' == character {
            parts = append(parts, current)
            current = ""

            continue
        }

        current = current + string(character)
    }

    return append(parts, current)
}

/* assertExampleProductPersisted creates a product through the api and reads the row back out of band. Without a database the example keeps the catalogue in memory on purpose, and there is then nothing to read out of band — the write still has to be accepted, which is what the assertion falls back to. */
func assertExampleProductPersisted(
    major exampleMajor,
    editor *exampleClient,
    application *exampleApplication,
    mysqlDsn string,
) string {
    body := `{"name":"e2e catalogue probe","description":"probe","categoryId":"cat-1","price":42.5,"currencyId":"cur-eur","stock":7}`

    response := editor.call("POST", exampleProductCreateRoute, "application/json", "application/json", body)
    if http.StatusOK != response.statusCode && http.StatusCreated != response.statusCode {
        fail(
            "[%s] %s: expected the write to be accepted, got %d: %s\n%s",
            major.label, exampleProductCreateRoute, response.statusCode, response.body, application.logTail(20),
        )
    }

    created := exampleProduct{}
    decodeExampleData(major.label, exampleProductCreateRoute, response.body, &created)

    if "" == created.Id {
        fail("[%s] %s: the write reported no identifier: %s", major.label, exampleProductCreateRoute, response.body)
    }

    if "" == mysqlDsn {
        skip(
            "[%s] product %q was not re-read out of band: MYSQL_DSN is cleared, so the catalogue is the in-memory one",
            major.label, created.Id,
        )

        return created.Id
    }

    database := openMysql(major.label, mysqlDsn)
    defer func() {
        _ = database.Close()
    }()

    row := struct {
        Name  string  `bun:"name"`
        Price float64 `bun:"price"`
        Stock int64   `bun:"stock"`
    }{}

    scanErr := database.
        NewSelect().
        Table(exampleProductTable(major)).
        Column("name", "price", "stock").
        Where("id = ?", created.Id).
        Limit(1).
        Scan(context.Background(), &row)
    if nil != scanErr {
        fail(
            "[%s] product %q is not in %s out of band (%v) — the api accepted a write that reached no database",
            major.label, created.Id, exampleProductTable(major), scanErr,
        )
    }

    if created.Name != row.Name || created.Price != row.Price || created.Stock != row.Stock {
        fail(
            "[%s] %s holds %q/%v/%d where the api reported %q/%v/%d",
            major.label, exampleProductTable(major), row.Name, row.Price, row.Stock, created.Name, created.Price, created.Stock,
        )
    }

    pass("[%s] the created product %q is in %s out of band, field for field", major.label, created.Id, exampleProductTable(major))

    return created.Id
}

/* assertExampleListingCached reads the catalogue, finds the listing in redis out of band, then changes the catalogue and finds it gone. Both halves matter: the first proves the listing is cached somewhere the application is not the only one that can see, and the second proves a write drops it rather than leaving a stale catalogue to be served. */
func assertExampleListingCached(
    major exampleMajor,
    editor *exampleClient,
    application *exampleApplication,
    redisAddress string,
    mysqlDsn string,
    postgresDsn string,
) {
    if "" == redisAddress {
        skip("[%s] the listing cache was not verified: REDIS_ADDRESS is cleared, so the key cannot be read out of band", major.label)

        return
    }

    redisClient := openRedis(redisAddress)
    defer redisClient.Close()

    storedKey := exampleCachePrefix(major) + exampleProductListCacheKey

    _, _ = redisClient.Do(
        context.Background(),
        redisClient.B().Del().Key(storedKey).Build(),
    ).AsInt64()

    listing := editor.call("GET", exampleProductListRoute, "application/json", "", "")
    if http.StatusOK != listing.statusCode {
        fail(
            "[%s] %s: expected 200, got %d: %s\n%s",
            major.label, exampleProductListRoute, listing.statusCode, listing.body, application.logTail(20),
        )
    }

    warm, existsErr := redisClient.Do(
        context.Background(),
        redisClient.B().Exists().Key(storedKey).Build(),
    ).AsInt64()
    if nil != existsErr {
        fail("[%s] could not ask redis for %q out of band (%v)", major.label, storedKey, existsErr)
    }

    if 1 != warm {
        fail(
            "[%s] the listing answered but %q is not in redis, so the catalogue is not cached where anything else can see it",
            major.label, storedKey,
        )
    }

    body := `{"name":"e2e cache probe","description":"probe","categoryId":"cat-1","price":1,"currencyId":"cur-eur","stock":1}`

    invalidating := editor.call("POST", exampleProductCreateRoute, "application/json", "application/json", body)
    if http.StatusOK != invalidating.statusCode && http.StatusCreated != invalidating.statusCode {
        fail("[%s] %s: the invalidating write was refused with %d: %s", major.label, exampleProductCreateRoute, invalidating.statusCode, invalidating.body)
    }

    invalidated := exampleProduct{}
    decodeExampleData(major.label, exampleProductCreateRoute, invalidating.body, &invalidated)

    cold, existsErr := redisClient.Do(
        context.Background(),
        redisClient.B().Exists().Key(storedKey).Build(),
    ).AsInt64()
    if nil != existsErr {
        fail("[%s] could not ask redis for %q out of band after the write (%v)", major.label, storedKey, existsErr)
    }

    if 0 != cold {
        fail(
            "[%s] %q survived a write, so the next listing would serve a catalogue that no longer exists",
            major.label, storedKey,
        )
    }

    pass("[%s] the listing was cached under %q and the write dropped it, both read out of band", major.label, storedKey)

    removeExampleProbeRows(major, invalidated.Id, mysqlDsn, postgresDsn)
}

/* assertExampleJournalRecordedTheWrite reads the journal straight out of its own database. The entry is not exposed by the route that created it, so this half of the check cannot be satisfied by the same code path that wrote it — and for the first major the door is postgres, which is also what proves the second live connection carried the write while the catalogue's mysql half carried the product. */
func assertExampleJournalRecordedTheWrite(major exampleMajor, editor *exampleClient, productId string, mysqlDsn string, postgresDsn string) {
    journalDsn, journalDsnVariable := exampleJournalDsn(major, mysqlDsn, postgresDsn)
    if "" == journalDsn {
        skip("[%s] the journal was not read out of band: %s is cleared", major.label, journalDsnVariable)

        return
    }

    database := openExampleJournal(major, journalDsn)
    defer func() {
        _ = database.Close()
    }()

    row := struct {
        Actor   string `bun:"actor"`
        Action  string `bun:"action"`
        Subject string `bun:"subject"`
    }{}

    scanErr := database.
        NewSelect().
        Table(exampleJournalTable(major)).
        Column("actor", "action", "subject").
        Where("subject_id = ?", productId).
        Where("action = ?", "created").
        Limit(1).
        Scan(context.Background(), &row)
    if nil != scanErr {
        fail(
            "[%s] the creation of %q left no entry in %s (%v) — a change to the nomenclature went unrecorded",
            major.label, productId, exampleJournalTable(major), scanErr,
        )
    }

    if "product" != row.Subject {
        fail("[%s] the journal recorded subject %q for a product write", major.label, row.Subject)
    }

    /* the seeded editor is user-2, and the journal records the identifier the token carries rather than the display name, so a write attributed to the system would mean the actor was lost on the way */
    if "" == row.Actor || "system" == row.Actor {
        fail(
            "[%s] the journal attributed a signed-in editor's write to %q, so the actor was not carried",
            major.label, row.Actor,
        )
    }

    pass("[%s] the journal recorded %s of product %q by %q, read out of band", major.label, row.Action, productId, row.Actor)
}

/* assertExampleWritesAreRateLimited spends the budget on writes the handler refuses anyway, so the assertion costs the catalogue nothing: the middleware runs before the handler, so a body that fails validation still consumes the allowance. */
func assertExampleWritesAreRateLimited(major exampleMajor, editor *exampleClient, redisAddress string) {
    if "" == redisAddress {
        skip("[%s] the write budget was not verified: REDIS_ADDRESS is cleared, so there is no limiter and no counter to reset", major.label)

        return
    }

    /* the counter is keyed by client address, and every major calls from the same one, so without a reset the second major starts with the first one's budget already spent */
    resetExampleRateLimitCounters(major.label, redisAddress, exampleMajorRateLimitPrefix(major))

    refused := `{"name":""}`

    for attempt := 1; attempt <= exampleWriteAllowance; attempt = attempt + 1 {
        response := editor.call("POST", exampleProductCreateRoute, "application/json", "application/json", refused)
        if http.StatusTooManyRequests == response.statusCode {
            fail(
                "[%s] %s: write %d of %d was refused with 429 before the allowance was spent",
                major.label, exampleProductCreateRoute, attempt, exampleWriteAllowance,
            )
        }
    }

    exhausted := editor.call("POST", exampleProductCreateRoute, "application/json", "application/json", refused)
    if http.StatusTooManyRequests != exhausted.statusCode {
        fail(
            "[%s] %s: expected 429 on write %d, got %d",
            major.label, exampleProductCreateRoute, exampleWriteAllowance+1, exhausted.statusCode,
        )
    }

    /* a read must still pass while the writes are refused: the budget is on what changes the catalogue */
    reading := editor.call("GET", exampleProductListRoute, "application/json", "", "")
    if http.StatusOK != reading.statusCode {
        fail(
            "[%s] %s: the listing answered %d while the write budget was spent — the limit reached the reads",
            major.label, exampleProductListRoute, reading.statusCode,
        )
    }

    resetExampleRateLimitCounters(major.label, redisAddress, exampleMajorRateLimitPrefix(major))

    pass(
        "[%s] the writes allowed exactly %d and refused the next with 429, while the listing kept answering",
        major.label, exampleWriteAllowance,
    )
}

/* removeExampleProbeRows takes this run's rows away again. The example applications share their development backends, and a probe left behind would be counted by the next run's catalogue reading. The journal rows live in the journal's own database, which for the first major is not the one holding the product. */
func removeExampleProbeRows(major exampleMajor, productId string, mysqlDsn string, postgresDsn string) {
    if "" == productId {
        return
    }

    journalDsn, _ := exampleJournalDsn(major, mysqlDsn, postgresDsn)
    if "" != journalDsn {
        journalDatabase := openExampleJournal(major, journalDsn)

        _, journalErr := journalDatabase.
            NewDelete().
            Table(exampleJournalTable(major)).
            Where("subject_id = ?", productId).
            Exec(context.Background())

        _ = journalDatabase.Close()

        if nil != journalErr {
            fail("[%s] the journal rows this run created could not be removed (%v)", major.label, journalErr)
        }
    }

    if "" == mysqlDsn {
        return
    }

    database := openMysql(major.label, mysqlDsn)
    defer func() {
        _ = database.Close()
    }()

    _, productErr := database.
        NewDelete().
        Table(exampleProductTable(major)).
        Where("id = ?", productId).
        Exec(context.Background())
    if nil != productErr {
        fail("[%s] the product this run created could not be removed (%v)", major.label, productErr)
    }
}
