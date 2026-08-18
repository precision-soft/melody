package main

import (
    "context"
    "io"
    "net/http"
    "strings"

    "github.com/uptrace/bun"
)

const scopedServiceLabel = "scoped service"

/* the journal table the v3 example writes its changes into, and the prefix of its redis write budget. Both are
the application's own, read here from the outside. */
const (
    scopedServiceRateLimitKey   = "melody-example-v3:rate_limit:"
    scopedServiceProbeNameFirst = "e2e scoped trail probe one"
    scopedServiceProbeNameOther = "e2e scoped trail probe two"
)

/* scopedServiceProbeNames are the products this section writes. They are removed with everything the
application recorded about them, because the example recycles a freed identifier into the next probe. */
var scopedServiceProbeNames = []string{scopedServiceProbeNameFirst, scopedServiceProbeNameOther}

/* scopedJournalRow is one row of the application's journal, read through the harness's own connection rather
than through any route the application serves. */
type scopedJournalRow struct {
    RequestId string
    Actor     string
    Action    string
    Subject   string
    SubjectId string
}

/* runScopedServiceCheck drives the scope-owned service the example declares through a `//melody:scoped`
constructor, and proves what it is for rather than that it exists.

The trail is the application's journal accumulator. The event listener that follows a write RECORDS into it;
the flush middleware, once the handler chain has returned, READS it and writes the batch. Both reach it through
the scope, and nothing else does. That gives the two claims a scope makes, each provable from the outside:

  - ONE INSTANCE PER REQUEST. If the listener's resolution and the middleware's ever yielded different objects,
    the middleware would flush a trail nobody wrote to and the journal row would simply not exist — while the
    response still carried its 201 and looked perfectly correct. So the row existing is the proof, and it is a
    proof a response cannot fabricate about itself.
  - NO INSTANCE BETWEEN REQUESTS. Every entry carries the request that caused it. Two writes must leave two
    rows whose request ids differ, and each must match the X-Request-Id its own response reported. A trail
    carried over from one request to the next would stamp the second request's change with the first
    request's identity.

The rows are read straight out of mysql. The application's own report of what it journalled would pass just as
well if the write never left the process.

The section also asserts the actor: the trail takes it from the security context of the request it belongs to,
so a change made by a signed-in editor may not be recorded as the system. */
func runScopedServiceCheck(baseUrl string, mysqlDsn string, redisAddress string) {
    if "" == mysqlDsn {
        skip(
            "%s: MYSQL_DSN is cleared — the journal rows the trail flushed were NOT read out of band, so neither the per-request instance nor the request stamping was checked",
            scopedServiceLabel,
        )

        return
    }

    if "" == redisAddress {
        skip(
            "%s: REDIS_ADDRESS is cleared, so the write budget an earlier section spent cannot be reset and the two probe writes would be refused",
            scopedServiceLabel,
        )

        return
    }

    database := openMysql(scopedServiceLabel, mysqlDsn)
    defer func() {
        _ = database.Close()
    }()

    /* a run that failed midway leaves its probes behind, and the assertions below count rows */
    removeScopedServiceProbes(database)
    defer removeScopedServiceProbes(database)

    /* EXAMPLE OVER HTTP asserts an exact exhaustion point of this budget just before this section runs, so the
       two writes below start from a budget of their own rather than from whatever is left of one */
    resetExampleRateLimitCounters(scopedServiceLabel, redisAddress, scopedServiceRateLimitKey)

    client := newExampleHttpClient()
    signInExampleHttpEditor(client, baseUrl, "")

    firstProductId, firstRequestId, firstChangeCount := createScopedServiceProbe(client, baseUrl, scopedServiceProbeNameFirst)
    otherProductId, otherRequestId, otherChangeCount := createScopedServiceProbe(client, baseUrl, scopedServiceProbeNameOther)

    /* the header counts what the flush ACTUALLY WROTE, measured across the flush inside the request. That is
       the one thing a journal row alone cannot show: the trail's own Close persists the same entries from the
       scope's teardown, which runs after the response has already gone to the client, so a row read afterwards
       would be there either way. A zero here with a row in the table means the write moved back behind the
       response and every caller that reads the journal on receipt of its 201 is racing it. */
    if "1" != firstChangeCount || "1" != otherChangeCount {
        fail(
            "%s: the responses reported %q and %q journal writes, wanted one each — the change was not written while the request was still being served",
            scopedServiceLabel,
            firstChangeCount,
            otherChangeCount,
        )
    }

    pass("each response reported the one journal write its own request performed, so the entry was committed before the caller was answered")

    if firstRequestId == otherRequestId {
        fail("%s: both writes reported request id %q, so they cannot be told apart", scopedServiceLabel, firstRequestId)
    }

    firstRow := requireScopedServiceJournalRow(database, firstProductId)
    otherRow := requireScopedServiceJournalRow(database, otherProductId)

    pass("the change each request made reached the journal, so the listener and the flush middleware held the same scope-owned trail")

    if firstRequestId != firstRow.RequestId {
        fail(
            "%s: the journal entry for %s carries request id %q while its response reported %q — the trail stamped a change with another request's identity",
            scopedServiceLabel,
            firstProductId,
            firstRow.RequestId,
            firstRequestId,
        )
    }

    if otherRequestId != otherRow.RequestId {
        fail(
            "%s: the journal entry for %s carries request id %q while its response reported %q",
            scopedServiceLabel,
            otherProductId,
            otherRow.RequestId,
            otherRequestId,
        )
    }

    if firstRow.RequestId == otherRow.RequestId {
        fail(
            "%s: both journal entries carry request id %q — one request's trail survived into the next",
            scopedServiceLabel,
            firstRow.RequestId,
        )
    }

    pass(
        "the two writes left two entries stamped with their own requests (%s, %s), so no trail was carried between them",
        firstRow.RequestId,
        otherRow.RequestId,
    )

    /* the journal records WHO by identifier rather than by the name they signed in under, so the editor's own
       identifier is read out of the directory instead of assumed */
    editorIdentifier := requireScopedServiceEditorIdentifier(database)

    for _, row := range []scopedJournalRow{firstRow, otherRow} {
        if "system" == row.Actor {
            fail(
                "%s: the entry for %s is attributed to the system although a signed-in editor made the change — the trail did not read the security context of its own request",
                scopedServiceLabel,
                row.SubjectId,
            )
        }

        if editorIdentifier != row.Actor {
            fail(
                "%s: the entry for %s names actor %q, wanted the signed-in editor %q — an entry the journal cannot attribute answers nothing",
                scopedServiceLabel,
                row.SubjectId,
                row.Actor,
                editorIdentifier,
            )
        }

        if "created" != row.Action || "product" != row.Subject {
            fail(
                "%s: the entry for %s records %q on %q, wanted a created product",
                scopedServiceLabel,
                row.SubjectId,
                row.Action,
                row.Subject,
            )
        }
    }

    pass(
        "both entries name the editor that made the change (%s) and what was done, read from mysql rather than from the application",
        editorIdentifier,
    )
}

/* requireScopedServiceEditorIdentifier reads the identifier of the account the section signs in as. The journal
records who acted by identifier, not by the name typed at the login form, so the expected value comes from the
directory the application authenticated against rather than from a constant that would drift with the seed. */
func requireScopedServiceEditorIdentifier(database *bun.DB) string {
    identifiers := make([]string, 0, 1)

    selectErr := database.
        NewSelect().
        ColumnExpr("id").
        Table(exampleV3UserTable).
        Where("username = ?", exampleHttpEditorUsername).
        Scan(context.Background(), &identifiers)
    if nil != selectErr {
        fail("%s: look up the editor in %s: %v", scopedServiceLabel, exampleV3UserTable, selectErr)
    }

    if 1 != len(identifiers) {
        fail(
            "%s: the directory holds %d accounts named %q, so the expected actor cannot be named",
            scopedServiceLabel,
            len(identifiers),
            exampleHttpEditorUsername,
        )
    }

    return identifiers[0]
}

/* createScopedServiceProbe writes one product and reports the identifier it was given, the request id the
response carried, and what the flush middleware said it wrote. The raw response is needed for the headers, so
this does not go through the shared client helpers. */
func createScopedServiceProbe(client *http.Client, baseUrl string, name string) (string, string, string) {
    body := `{"name":"` + name + `","description":"probe","categoryId":"cat-1","price":4,"currencyId":"cur-eur","stock":1}`

    request, requestErr := http.NewRequest(
        "POST",
        strings.TrimRight(baseUrl, "/")+"/products/api/create/",
        strings.NewReader(body),
    )
    if nil != requestErr {
        fail("%s: build the create request: %v", scopedServiceLabel, requestErr)
    }

    request.Header.Set("Content-Type", "application/json")
    request.Header.Set("Accept", "application/json")

    response, responseErr := client.Do(request)
    if nil != responseErr {
        fail("%s: create %q: %v", scopedServiceLabel, name, responseErr)
    }
    defer response.Body.Close()

    payload, readErr := io.ReadAll(response.Body)
    if nil != readErr {
        fail("%s: read the create response for %q: %v", scopedServiceLabel, name, readErr)
    }

    if http.StatusUnauthorized == response.StatusCode || http.StatusForbidden == response.StatusCode {
        fail(
            "%s: the write was refused with %d — the section reached the firewall rather than the nomenclature, so nothing below was measured",
            scopedServiceLabel,
            response.StatusCode,
        )
    }

    if http.StatusTooManyRequests == response.StatusCode {
        fail(
            "%s: the write was refused with 429 — the budget was not reset before this section, so the trail was never exercised",
            scopedServiceLabel,
        )
    }

    if http.StatusOK != response.StatusCode && http.StatusCreated != response.StatusCode {
        fail("%s: %q was refused with %d: %s", scopedServiceLabel, name, response.StatusCode, exampleTruncate(string(payload)))
    }

    created := exampleProduct{}
    decodeExampleData(scopedServiceLabel, name, string(payload), &created)

    if "" == created.Id {
        fail("%s: the write of %q reported no identifier: %s", scopedServiceLabel, name, exampleTruncate(string(payload)))
    }

    requestId := response.Header.Get("X-Request-Id")
    if "" == requestId {
        fail("%s: the response for %q carried no X-Request-Id, so the journal entry cannot be tied to it", scopedServiceLabel, name)
    }

    return created.Id, requestId, response.Header.Get("X-Example-Catalog-Changes")
}

/* requireScopedServiceJournalRow reads the one entry the application must have written for a product, through
the harness's own connection. Exactly one is demanded: a second entry for the same change would mean the trail
was flushed twice. */
func requireScopedServiceJournalRow(database *bun.DB, productId string) scopedJournalRow {
    rows := make([]scopedJournalRow, 0, 2)

    selectErr := database.
        NewSelect().
        ColumnExpr("request_id AS request_id").
        ColumnExpr("actor AS actor").
        ColumnExpr("action AS action").
        ColumnExpr("subject AS subject").
        ColumnExpr("subject_id AS subject_id").
        Table(exampleV3JournalTable).
        Where("subject = ?", "product").
        Where("subject_id = ?", productId).
        Scan(context.Background(), &rows)
    if nil != selectErr {
        fail("%s: read the journal entry for %s out of band: %v", scopedServiceLabel, productId, selectErr)
    }

    if 0 == len(rows) {
        fail(
            "%s: the write of %s left NO journal entry — the flush middleware and the event listener did not share one trail, or the flush never ran",
            scopedServiceLabel,
            productId,
        )
    }

    if 1 != len(rows) {
        fail(
            "%s: the write of %s left %d journal entries, wanted one — the trail was flushed more than once",
            scopedServiceLabel,
            productId,
            len(rows),
        )
    }

    return rows[0]
}

func removeScopedServiceProbes(database *bun.DB) {
    removeExampleV3ProductProbes(scopedServiceLabel, database, scopedServiceProbeNames)
}
