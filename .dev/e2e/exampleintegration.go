package main

import (
    "context"
    "encoding/json"
    "net/http"
    "time"
)

/* the demo prefixes the v1 and v2 examples write under. They are deliberately not the ones the v3 example
uses: the three applications share one redis in development, and EXAMPLE OVER HTTP asserts the exact
exhaustion point of the v3 rate limiter, which a shared counter would spend. */
const (
    exampleIntegrationCachePrefix     = "melody-example-app:demo:"
    exampleIntegrationRateLimitPrefix = "melody-example-app:rate_limit:"

    exampleIntegrationCacheRoute     = "/integration/cache/"
    exampleIntegrationDatabaseRoute  = "/integration/database/"
    exampleIntegrationRateLimitRoute = "/integration/ratelimit/"
    exampleIntegrationReportRoute    = "/integration/report/"

    exampleIntegrationRateLimitAllowance = 5
)

type exampleEnvelope struct {
    Success bool            `json:"success"`
    Payload json.RawMessage `json:"payload"`
}

type exampleCacheDemo struct {
    Key   string `json:"key"`
    Wrote string `json:"wrote"`
    Read  string `json:"read"`
    Hit   bool   `json:"hit"`
}

type exampleDatabaseDemo struct {
    Id         int64  `json:"id"`
    Note       string `json:"note"`
    RecordedAt string `json:"recordedAt"`
    Total      int    `json:"total"`
}

type exampleReportDemo struct {
    RecordedAt string `json:"recordedAt"`
    Payload    string `json:"payload"`
    FromCache  bool   `json:"fromCache"`
}

/* decodeExampleData pulls the payload out of the example's response envelope. The presenter wraps every api
answer, so a decode straight into the payload type would silently yield a zero value and turn a broken demo
into a passing assertion. */
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

/* runExampleIntegrationAssertions drives the live-backend demos of one example application. It runs inside the
per-major section, with the client the login flow already authenticated, because the demo routes sit under the
example's ROLE_USER catch-all.

Every check is made twice over: once through the application, and once out of band through the harness's own
connection to the same backend. The application's own write and read pass through one code path, so a broken
pair would cancel out and report success; the out-of-band half is what makes the claim about the backend
rather than about the handler. */
func runExampleIntegrationAssertions(
    major exampleMajor,
    client *exampleClient,
    application *exampleApplication,
    redisAddress string,
    mysqlDsn string,
) {
    assertExampleReportDemo(major, client)
    assertExampleCacheDemo(major, client, application, redisAddress)
    assertExampleDatabaseDemo(major, client, application, mysqlDsn)
    assertExampleRateLimitDemo(major, client, redisAddress)
}

/* assertExampleReportDemo covers the clock-driven report, which needs no backend at all: it is the one demo
that must answer on every machine, so it is also the proof that the integration routes are wired. */
func assertExampleReportDemo(major exampleMajor, client *exampleClient) {
    response := client.call("GET", exampleIntegrationReportRoute, "application/json", "", "")
    if http.StatusOK != response.statusCode {
        fail("[%s] %s: expected 200, got %d: %s", major.label, exampleIntegrationReportRoute, response.statusCode, response.body)
    }

    report := exampleReportDemo{}
    decodeExampleData(major.label, exampleIntegrationReportRoute, response.body, &report)

    if "" == report.RecordedAt {
        fail("[%s] %s: the report carried no clock stamp: %s", major.label, exampleIntegrationReportRoute, response.body)
    }

    if _, parseErr := time.Parse(time.RFC3339, report.RecordedAt); nil != parseErr {
        fail("[%s] %s: the clock stamp is not a timestamp (%v): %q", major.label, exampleIntegrationReportRoute, parseErr, report.RecordedAt)
    }

    pass("[%s] the clock-stamped catalogue report answered with %q", major.label, report.RecordedAt)
}

func assertExampleCacheDemo(major exampleMajor, client *exampleClient, application *exampleApplication, redisAddress string) {
    if "" == redisAddress {
        skip("[%s] the redis cache demo was not verified: REDIS_ADDRESS is cleared, so the harness cannot read the key back out of band", major.label)

        return
    }

    response := client.call("GET", exampleIntegrationCacheRoute, "application/json", "", "")
    if http.StatusOK != response.statusCode {
        fail(
            "[%s] %s: expected 200, got %d. The harness has redis, so the example should have wired its cache demo; the application log tail follows:\n%s\n%s",
            major.label, exampleIntegrationCacheRoute, response.statusCode, response.body, application.logTail(20),
        )
    }

    demo := exampleCacheDemo{}
    decodeExampleData(major.label, exampleIntegrationCacheRoute, response.body, &demo)

    if false == demo.Hit {
        fail("[%s] %s: the example wrote a key and did not read it back", major.label, exampleIntegrationCacheRoute)
    }

    if demo.Wrote != demo.Read {
        fail("[%s] %s: the example read back %q having written %q", major.label, exampleIntegrationCacheRoute, demo.Read, demo.Wrote)
    }

    redisClient := openRedis(redisAddress)
    defer redisClient.Close()

    storedKey := exampleIntegrationCachePrefix + demo.Key

    stored, getErr := redisClient.Do(
        context.Background(),
        redisClient.B().Get().Key(storedKey).Build(),
    ).ToString()
    if nil != getErr {
        fail("[%s] %s: the key %q is not in redis out of band (%v)", major.label, exampleIntegrationCacheRoute, storedKey, getErr)
    }

    if demo.Wrote != stored {
        fail("[%s] %s: redis holds %q where the example reported writing %q", major.label, exampleIntegrationCacheRoute, stored, demo.Wrote)
    }

    pass("[%s] the redis cache demo round-tripped and the key is in redis out of band", major.label)
}

func assertExampleDatabaseDemo(major exampleMajor, client *exampleClient, application *exampleApplication, mysqlDsn string) {
    response := client.call("GET", exampleIntegrationDatabaseRoute, "application/json", "", "")
    if http.StatusOK != response.statusCode {
        fail(
            "[%s] %s: expected 200, got %d: %s\n%s",
            major.label, exampleIntegrationDatabaseRoute, response.statusCode, response.body, application.logTail(20),
        )
    }

    first := exampleDatabaseDemo{}
    decodeExampleData(major.label, exampleIntegrationDatabaseRoute, response.body, &first)

    if 0 >= first.Id {
        fail("[%s] %s: the insert reported no identifier: %s", major.label, exampleIntegrationDatabaseRoute, response.body)
    }

    secondResponse := client.call("GET", exampleIntegrationDatabaseRoute, "application/json", "", "")
    second := exampleDatabaseDemo{}
    decodeExampleData(major.label, exampleIntegrationDatabaseRoute, secondResponse.body, &second)

    if first.Total+1 != second.Total {
        fail(
            "[%s] %s: the table held %d notes and then %d; one write should have added exactly one row",
            major.label, exampleIntegrationDatabaseRoute, first.Total, second.Total,
        )
    }

    if "" == mysqlDsn {
        skip(
            "[%s] the note rows were not re-read out of band and were left behind: MYSQL_DSN is cleared",
            major.label,
        )

        return
    }

    database := openMysql(major.label, mysqlDsn)
    defer func() {
        _ = database.Close()
    }()

    row := struct {
        Note       string    `bun:"note"`
        RecordedAt time.Time `bun:"recorded_at"`
    }{}

    scanErr := database.
        NewSelect().
        Table("melody_example_notes").
        Column("note", "recorded_at").
        Where("id = ?", first.Id).
        Limit(1).
        Scan(context.Background(), &row)
    if nil != scanErr {
        fail("[%s] %s: note %d is not in mysql out of band (%v)", major.label, exampleIntegrationDatabaseRoute, first.Id, scanErr)
    }

    if first.Note != row.Note {
        fail("[%s] %s: mysql holds note %q where the example reported %q", major.label, exampleIntegrationDatabaseRoute, row.Note, first.Note)
    }

    /* the two applications share one table in development, so each run takes its own rows away again */
    _, deleteErr := database.
        NewDelete().
        Table("melody_example_notes").
        Where("id IN (?, ?)", first.Id, second.Id).
        Exec(context.Background())
    if nil != deleteErr {
        fail("[%s] %s: the rows this run created could not be removed (%v)", major.label, exampleIntegrationDatabaseRoute, deleteErr)
    }

    pass("[%s] the mysql demo wrote note %d, read it back out of band and cleaned up after itself", major.label, first.Id)
}

func assertExampleRateLimitDemo(major exampleMajor, client *exampleClient, redisAddress string) {
    if "" == redisAddress {
        skip("[%s] the rate-limit demo was not verified: REDIS_ADDRESS is cleared, so the counter cannot be reset first", major.label)

        return
    }

    /* the counter is keyed by client ip, and both majors call from the same address through the same prefix, so
    without a reset the second major starts with the first one's budget already spent */
    resetExampleRateLimitCounters(major.label, redisAddress, exampleIntegrationRateLimitPrefix)

    for attempt := 1; attempt <= exampleIntegrationRateLimitAllowance; attempt = attempt + 1 {
        response := client.call("GET", exampleIntegrationRateLimitRoute, "application/json", "", "")
        if http.StatusOK != response.statusCode {
            fail(
                "[%s] %s: request %d of %d was refused with %d before the allowance was spent",
                major.label, exampleIntegrationRateLimitRoute, attempt, exampleIntegrationRateLimitAllowance, response.statusCode,
            )
        }
    }

    exhausted := client.call("GET", exampleIntegrationRateLimitRoute, "application/json", "", "")
    if http.StatusTooManyRequests != exhausted.statusCode {
        fail(
            "[%s] %s: expected 429 on request %d, got %d",
            major.label, exampleIntegrationRateLimitRoute, exampleIntegrationRateLimitAllowance+1, exhausted.statusCode,
        )
    }

    pass(
        "[%s] the redis rate limiter allowed exactly %d requests and refused the next with 429",
        major.label, exampleIntegrationRateLimitAllowance,
    )
}
