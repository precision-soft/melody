package main

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "net/url"
    "strconv"
    "strings"
    "time"

    "github.com/uptrace/bun"
)

const (
    exampleCategoryReadRoute   = "/categories/api/read/"
    exampleCategoryCreateRoute = "/categories/api/create/"
    exampleCategoryUpdateRoute = "/categories/api/update/"
    exampleCategoryDeleteRoute = "/categories/api/delete/"

    /* cat-1 is the category the seeded prod-1 sits in */
    exampleSeededCategoryInUse = "cat-1"
    exampleSeededProduct       = "prod-1"
    exampleProductReadRoute    = "/products/api/read/"

    exampleUserDirectoryTopic = "user-directory"
    exampleUserListPage       = "/users/"

    exampleEditorRole = "ROLE_EDITOR"
    /* granted on top of the editor's role: it moves the account's revision, and an account holding the admin's role could not be removed by the admin */
    exampleUserRole   = "ROLE_USER"

    /* the workspace boots the application with this re-read interval, so a stream that must end does so within two of them */
    exampleStreamRevisionInterval = time.Second
    exampleStreamRevisionKey      = "APP_EVENT_STREAM_REVISION_INTERVAL"
    exampleStreamClosedBudget     = 2*exampleStreamRevisionInterval + 2*time.Second

    exampleDispatchSuccessLine = "consumed own messages: 3 of 3"
    exampleDispatchedLine      = "dispatched welcome email for user:"
    exampleDispatchForeignLine = "consumed a message of another producer"
)

/* runExampleCatalogueSliceAssertions drives the catalogue slice of the fourth major's example over the wire and its console: the category write doors, the views per window, the topic gate, the stream's account revision and the dispatch command counting its own messages */
func runExampleCatalogueSliceAssertions(major exampleMajor, workspace string, redisAddress string, mysqlDsn string) {
    if "" == redisAddress {
        skip("[%s] REDIS_ADDRESS is cleared, so the write budget cannot be reset and the catalogue slice was not driven", major.label)

        return
    }

    label := fmt.Sprintf("[%s] catalogue slice", major.label)
    rateLimitPrefix := exampleMajorRateLimitPrefix(major)

    resetExampleRateLimitCounters(label, redisAddress, rateLimitPrefix)
    defer resetExampleRateLimitCounters(label, redisAddress, rateLimitPrefix)

    editor := newExampleClient(major)
    signInExampleMajorAs(label, editor, exampleEditorUsername, exampleEditorPassword)

    assertExampleCategoryWriteDoors(label, major, editor, mysqlDsn)
    assertExampleViewsPerWindow(label, major, workspace, editor)
    assertExampleTopicGate(label, major, editor)

    resetExampleRateLimitCounters(label, redisAddress, rateLimitPrefix)
    assertExampleStreamEndsWithTheAccount(label, major, workspace, mysqlDsn)

    assertExampleDispatchCountsItsOwn(label, major, workspace)
}

func signInExampleMajorAs(label string, client *exampleClient, username string, password string) {
    signedIn := client.call("POST", exampleSignInRoute(client.major), "application/json", "application/json", exampleCredentialBody(username, password))
    if http.StatusOK != signedIn.statusCode {
        fail("%s: %s could not sign in (%d): %s", label, username, signedIn.statusCode, exampleTruncate(signedIn.body))
    }
}

/* exampleWrite sends a write with the forgery token of the token route, as the page's script does */
func exampleWrite(client *exampleClient, method string, path string, body string) exampleResponse {
    token := exampleCsrfTokenFromRoute(client.major, client)

    return client.callWithHeaderList(method, path, "application/json", "application/json", map[string]string{exampleCsrfHeaderName: token}, body)
}

func exampleCategoryNames(label string, client *exampleClient) map[string]string {
    listing := client.call("GET", exampleCategoryReadRoute, "application/json", "", "")
    if http.StatusOK != listing.statusCode {
        fail("%s: %s answered %d: %s", label, exampleCategoryReadRoute, listing.statusCode, exampleTruncate(listing.body))
    }

    envelope := struct {
        Payload []struct {
            Id   string `json:"id"`
            Name string `json:"name"`
        } `json:"payload"`
    }{}
    if decodeErr := json.Unmarshal([]byte(listing.body), &envelope); nil != decodeErr {
        fail("%s: %s answered no category list (%v): %s", label, exampleCategoryReadRoute, decodeErr, exampleTruncate(listing.body))
    }

    names := make(map[string]string, len(envelope.Payload))
    for _, category := range envelope.Payload {
        names[category.Id] = category.Name
    }

    return names
}

/* the editor creates, renames and deletes a category of its own and the cached listing shows each write; a category a product sits in is refused 409 and a write by ROLE_USER 403 */
func assertExampleCategoryWriteDoors(label string, major exampleMajor, editor *exampleClient, mysqlDsn string) {
    categoryId := liveExampleUnique("e2e-category")

    /* the id a refused ROLE_USER create names is removed too, in case the refusal did not hold */
    removeCategoryOnFailure := pushFailureCleanup(func() {
        _ = exampleWrite(editor, "DELETE", exampleCategoryDeleteRoute+categoryId+"/", "")
        _ = exampleWrite(editor, "DELETE", exampleCategoryDeleteRoute+categoryId+"-user/", "")
    })
    defer removeCategoryOnFailure()

    if _, listed := exampleCategoryNames(label, editor)[categoryId]; true == listed {
        fail("%s: the listing named %s before it was created", label, categoryId)
    }

    created := exampleWrite(editor, "POST", exampleCategoryCreateRoute, `{"id":"`+categoryId+`","name":"E2E Category"}`)
    if http.StatusCreated != created.statusCode {
        fail("%s: the editor's create answered %d, wanted 201: %s", label, created.statusCode, exampleTruncate(created.body))
    }
    if name := exampleCategoryNames(label, editor)[categoryId]; "E2E Category" != name {
        fail("%s: the listing after the create named %s %q, wanted \"E2E Category\"", label, categoryId, name)
    }

    renamed := exampleWrite(editor, "PUT", exampleCategoryUpdateRoute+categoryId+"/", `{"name":"E2E Renamed"}`)
    if http.StatusOK != renamed.statusCode {
        fail("%s: the editor's rename answered %d, wanted 200: %s", label, renamed.statusCode, exampleTruncate(renamed.body))
    }
    if name := exampleCategoryNames(label, editor)[categoryId]; "E2E Renamed" != name {
        fail("%s: the listing after the rename named %s %q, wanted \"E2E Renamed\"", label, categoryId, name)
    }

    inUse := exampleWrite(editor, "DELETE", exampleCategoryDeleteRoute+exampleSeededCategoryInUse+"/", "")
    if http.StatusConflict != inUse.statusCode {
        fail("%s: deleting %s, which %s sits in, answered %d, wanted 409: %s", label, exampleSeededCategoryInUse, exampleSeededProduct, inUse.statusCode, exampleTruncate(inUse.body))
    }

    user := newExampleClient(major)
    signInExampleMajorAs(label, user, exampleUsername, examplePassword)
    if refused := exampleWrite(user, "POST", exampleCategoryCreateRoute, `{"id":"`+categoryId+`-user","name":"E2E User"}`); http.StatusForbidden != refused.statusCode {
        fail("%s: a create by the ROLE_USER account answered %d, wanted 403: %s", label, refused.statusCode, exampleTruncate(refused.body))
    }
    if refused := exampleWrite(user, "PUT", exampleCategoryUpdateRoute+categoryId+"/", `{"name":"E2E User"}`); http.StatusForbidden != refused.statusCode {
        fail("%s: a rename by the ROLE_USER account answered %d, wanted 403: %s", label, refused.statusCode, exampleTruncate(refused.body))
    }

    deleted := exampleWrite(editor, "DELETE", exampleCategoryDeleteRoute+categoryId+"/", "")
    if http.StatusOK != deleted.statusCode {
        fail("%s: the editor's delete answered %d, wanted 200: %s", label, deleted.statusCode, exampleTruncate(deleted.body))
    }
    if _, listed := exampleCategoryNames(label, editor)[categoryId]; true == listed {
        fail("%s: the listing after the delete still named %s", label, categoryId)
    }

    removeExampleRecordsOf(label, major, mysqlDsn, "category", categoryId)

    pass("%s: the editor created, renamed and deleted a category, each write shown by the listing; deleting %s, which a product sits in, answered 409 and a write by ROLE_USER 403", label, exampleSeededCategoryInUse)
}

type exampleProductViews struct {
    views         int64
    windowViews   int64
    windowSeconds int64
}

func readExampleProductViews(label string, client *exampleClient) exampleProductViews {
    read := client.call("GET", exampleProductReadRoute+exampleSeededProduct+"/", "application/json", "", "")
    if http.StatusOK != read.statusCode {
        fail("%s: reading %s answered %d: %s", label, exampleSeededProduct, read.statusCode, exampleTruncate(read.body))
    }

    envelope := struct {
        Payload struct {
            Views              *int64 `json:"views"`
            DailyViews         *int64 `json:"dailyViews"`
            ViewsWindowSeconds *int64 `json:"viewsWindowSeconds"`
        } `json:"payload"`
    }{}
    if decodeErr := json.Unmarshal([]byte(read.body), &envelope); nil != decodeErr {
        fail("%s: reading %s answered no payload (%v): %s", label, exampleSeededProduct, decodeErr, exampleTruncate(read.body))
    }
    if nil == envelope.Payload.Views || nil == envelope.Payload.DailyViews || nil == envelope.Payload.ViewsWindowSeconds {
        fail("%s: reading %s answered without views, dailyViews and viewsWindowSeconds: %s", label, exampleSeededProduct, exampleTruncate(read.body))
    }

    return exampleProductViews{views: *envelope.Payload.Views, windowViews: *envelope.Payload.DailyViews, windowSeconds: *envelope.Payload.ViewsWindowSeconds}
}

/* two reads raise both counts by one; product:list prints the two columns and does not raise them */
func assertExampleViewsPerWindow(label string, major exampleMajor, workspace string, editor *exampleClient) {
    first := readExampleProductViews(label, editor)
    second := readExampleProductViews(label, editor)

    if first.views+1 != second.views || first.windowViews+1 != second.windowViews {
        fail("%s: two reads of %s answered views %d then %d and dailyViews %d then %d, wanted each to rise by one", label, exampleSeededProduct, first.views, second.views, first.windowViews, second.windowViews)
    }
    if 0 >= second.windowSeconds {
        fail("%s: reading %s answered viewsWindowSeconds %d, wanted a positive window", label, exampleSeededProduct, second.windowSeconds)
    }

    listing := runExampleCommand(major, workspace, "product:list")
    columns := exampleProductListColumns(listing, exampleSeededProduct)
    if nil == columns {
        fail("%s: product:list printed no VIEWS and VIEWS_WINDOW columns with a row for %s:\n%s", label, exampleSeededProduct, exampleTail(listing, 20))
    }
    if strconv.FormatInt(second.views, 10) != columns["VIEWS"] || strconv.FormatInt(second.windowViews, 10) != columns["VIEWS_WINDOW"] {
        fail("%s: product:list printed VIEWS %q and VIEWS_WINDOW %q for %s, wanted the %d and %d the reads answered", label, columns["VIEWS"], columns["VIEWS_WINDOW"], exampleSeededProduct, second.views, second.windowViews)
    }

    third := readExampleProductViews(label, editor)
    if second.views+1 != third.views || second.windowViews+1 != third.windowViews {
        fail("%s: the read after product:list answered views %d and dailyViews %d, wanted %d and %d: the listing raised a counter", label, third.views, third.windowViews, second.views+1, second.windowViews+1)
    }

    pass("%s: two reads raised views and dailyViews by one each (window %ds); product:list printed both counts and raised neither", label, second.windowSeconds)
}

/* exampleProductListColumns answers the cells of one product's row by the header's names, nil when the header lacks a view column or the row is missing */
func exampleProductListColumns(listing string, productId string) map[string]string {
    var header []string
    for _, line := range strings.Split(listing, "\n") {
        cells := strings.Split(line, "|")
        for index := range cells {
            cells[index] = strings.TrimSpace(cells[index])
        }

        if "ID" == cells[0] {
            header = cells

            continue
        }

        if productId != cells[0] || len(header) != len(cells) {
            continue
        }

        columns := make(map[string]string, len(header))
        for index, name := range header {
            columns[name] = cells[index]
        }

        if _, found := columns["VIEWS"]; false == found {
            return nil
        }
        if _, found := columns["VIEWS_WINDOW"]; false == found {
            return nil
        }

        return columns
    }

    return nil
}

/* the editor may neither read nor write the user directory's topic; the admin's stream opens, and the users page subscribes to it */
func assertExampleTopicGate(label string, major exampleMajor, editor *exampleClient) {
    streamPath := serverSentEventStreamRoute + "?topic=" + exampleUserDirectoryTopic

    refusedStream := openExampleSessionStream(label, major, editor, streamPath)
    _ = refusedStream.Body.Close()
    if http.StatusForbidden != refusedStream.StatusCode {
        fail("%s: the editor's stream of %s answered %d, wanted 403", label, exampleUserDirectoryTopic, refusedStream.StatusCode)
    }

    refusedUpgrade := newLiveExampleClient(major.baseUrl()).openStream(label, "/ws?topic="+exampleUserDirectoryTopic, map[string]string{
        "Cookie":                exampleClientCookieHeader(label, editor),
        "Connection":            "Upgrade",
        "Upgrade":               "websocket",
        "Sec-WebSocket-Version": "13",
        "Sec-WebSocket-Key":     "dGhlIHNhbXBsZSBub25jZQ==",
        "Origin":                major.baseUrl(),
    })
    _ = refusedUpgrade.Body.Close()
    if http.StatusForbidden != refusedUpgrade.StatusCode {
        fail("%s: the editor's websocket upgrade on %s answered %d, wanted 403", label, exampleUserDirectoryTopic, refusedUpgrade.StatusCode)
    }

    if refusedPublish := exampleWrite(editor, "POST", serverSentEventPublishRoute+"?topic="+exampleUserDirectoryTopic+"&text=e2e", ""); http.StatusForbidden != refusedPublish.statusCode {
        fail("%s: the editor's publish onto %s answered %d, wanted 403: %s", label, exampleUserDirectoryTopic, refusedPublish.statusCode, exampleTruncate(refusedPublish.body))
    }

    admin := newExampleClient(major)
    signInExampleMajorAs(label, admin, exampleAdminUsername, exampleAdminPassword)

    adminStream := openExampleSessionStream(label, major, admin, streamPath)
    releaseAdminStream := pushFailureCleanup(func() {
        _ = adminStream.Body.Close()
    })
    if http.StatusOK != adminStream.StatusCode {
        fail("%s: the admin's stream of %s answered %d, wanted 200", label, exampleUserDirectoryTopic, adminStream.StatusCode)
    }
    if _, arrived := newServerSentEventStreamReader(adminStream.Body).next(serverSentEventArrivalBudget); false == arrived {
        fail("%s: the admin's stream of %s sent no preamble", label, exampleUserDirectoryTopic)
    }
    _ = adminStream.Body.Close()
    releaseAdminStream()

    usersPage := admin.call("GET", exampleUserListPage, "text/html", "", "")
    if http.StatusOK != usersPage.statusCode || false == strings.Contains(usersPage.body, "/ws?topic="+exampleUserDirectoryTopic) {
        fail("%s: %s answered %d to the admin without a socket on /ws?topic=%s", label, exampleUserListPage, usersPage.statusCode, exampleUserDirectoryTopic)
    }

    pass("%s: the editor's stream of, websocket upgrade on and publish onto %s answered 403, the admin's stream opened and the users page subscribes to it", label, exampleUserDirectoryTopic)
}

func openExampleSessionStream(label string, major exampleMajor, client *exampleClient, path string) *http.Response {
    return newLiveExampleClient(major.baseUrl()).openStream(label, path, map[string]string{
        "Accept": "text/event-stream",
        "Cookie": exampleClientCookieHeader(label, client),
    })
}

func exampleClientCookieHeader(label string, client *exampleClient) string {
    base, parseErr := url.Parse(client.major.baseUrl())
    if nil != parseErr {
        fail("%s: parse the base url: %v", label, parseErr)
    }

    rendered := []string{}
    for _, cookie := range client.jar.Cookies(base) {
        rendered = append(rendered, cookie.Name+"="+cookie.Value)
    }
    if 0 == len(rendered) {
        fail("%s: the signed-in client holds no cookie", label)
    }

    return strings.Join(rendered, "; ")
}

/* an account of its own opens a stream; signing that session out ends it, and a role granted from the console ends the next one, each within two re-read intervals and with the reason as the last comment */
func assertExampleStreamEndsWithTheAccount(label string, major exampleMajor, workspace string, mysqlDsn string) {
    admin := newExampleClient(major)
    signInExampleMajorAs(label, admin, exampleAdminUsername, exampleAdminPassword)

    username := liveExampleUnique("e2e-revision")
    password := "revision-password"

    created := exampleWrite(admin, "POST", "/users/api/create/", `{"username":"`+username+`","password":"`+password+`","roles":["`+exampleEditorRole+`"]}`)
    account := struct {
        Payload struct {
            Id string `json:"id"`
        } `json:"payload"`
    }{}
    _ = json.Unmarshal([]byte(created.body), &account)
    if http.StatusCreated != created.statusCode || "" == account.Payload.Id {
        fail("%s: creating the probe account answered %d: %s", label, created.statusCode, exampleTruncate(created.body))
    }

    removeAccount := func() {
        if removed := exampleWrite(admin, "DELETE", "/users/api/delete/"+account.Payload.Id+"/", ""); http.StatusOK != removed.statusCode {
            fail("%s: removing the probe account %s answered %d: %s", label, account.Payload.Id, removed.statusCode, exampleTruncate(removed.body))
        }
        removeExampleRecordsOf(label, major, mysqlDsn, "user", account.Payload.Id)
    }
    removeAccountOnFailure := pushFailureCleanup(removeAccount)

    streamPath := serverSentEventStreamRoute + "?topic=" + liveExampleUnique("e2e-revision")

    signedOut := newExampleClient(major)
    signInExampleMajorAs(label, signedOut, username, password)
    assertExampleStreamEndsAfter(label, major, signedOut, streamPath, "session-ended", func() {
        if loggedOut := signedOut.callWithHeaderList("POST", exampleLogoutRoute, "text/html", "", nil, ""); http.StatusFound != loggedOut.statusCode {
            fail("%s: signing the probe account out answered %d, wanted 302", label, loggedOut.statusCode)
        }
    })

    granted := newExampleClient(major)
    signInExampleMajorAs(label, granted, username, password)
    assertExampleStreamEndsAfter(label, major, granted, streamPath, "revision-changed", func() {
        output := runExampleCommand(major, workspace, "example:grant:role", "--role", exampleUserRole, username)
        if false == strings.Contains(output, "known=true") {
            fail("%s: example:grant:role did not find the probe account:\n%s", label, exampleTail(output, 20))
        }
    })

    removeAccountOnFailure()
    removeAccount()

    pass("%s: a signed-in stream ended within %s of its session's sign-out and of a role granted from the console, the reason its last comment", label, exampleStreamClosedBudget)
}

func assertExampleStreamEndsAfter(label string, major exampleMajor, client *exampleClient, streamPath string, reason string, change func()) {
    stream := openExampleSessionStream(label, major, client, streamPath)
    releaseStream := pushFailureCleanup(func() {
        _ = stream.Body.Close()
    })
    defer func() {
        releaseStream()
        _ = stream.Body.Close()
    }()

    if http.StatusOK != stream.StatusCode {
        fail("%s: the probe account's stream answered %d, wanted 200", label, stream.StatusCode)
    }

    reader := newServerSentEventStreamReader(stream.Body)
    if _, arrived := reader.next(serverSentEventArrivalBudget); false == arrived {
        fail("%s: the probe account's stream sent no preamble", label)
    }

    /* a stream that outlives a few re-reads unchanged proves the end below is the change's */
    if frame, arrived := reader.next(3 * exampleStreamRevisionInterval); true == arrived {
        fail("%s: the probe account's stream sent %s before anything changed", label, frame.describe())
    }

    change()

    deadline := time.Now().Add(exampleStreamClosedBudget)
    lastComment := ""
    for {
        frame, arrived := reader.next(time.Until(deadline))
        if false == arrived {
            break
        }
        if true == frame.isComment() {
            lastComment = frame.commentList[len(frame.commentList)-1]
        }
    }

    if time.Now().After(deadline) {
        fail("%s: the probe account's stream was still open %s after the change, wanted it closed with %q", label, exampleStreamClosedBudget, reason)
    }
    if "closed: "+reason != lastComment {
        fail("%s: the probe account's stream ended with the comment %q, wanted %q", label, lastComment, "closed: "+reason)
    }
}

/* --no-consume dispatches and leaves; the next run consumes the three it left as another producer's and counts its own three */
func assertExampleDispatchCountsItsOwn(label string, major exampleMajor, workspace string) {
    dispatchedOnly := runExampleCommand(major, workspace, "messagebus:dispatch", "--no-consume", "--timeout", "5s")
    if 3 != strings.Count(dispatchedOnly, exampleDispatchedLine) || true == strings.Contains(dispatchedOnly, "consumed ") {
        fail("%s: messagebus:dispatch --no-consume printed other than three dispatched lines and nothing consumed:\n%s", label, exampleTail(dispatchedOnly, 20))
    }

    consumed := runExampleCommand(major, workspace, "messagebus:dispatch", "--timeout", "5s")
    if 3 != strings.Count(consumed, exampleDispatchedLine) || false == strings.Contains(consumed, exampleDispatchSuccessLine) {
        fail("%s: messagebus:dispatch --timeout 5s printed no %q after three dispatched lines:\n%s", label, exampleDispatchSuccessLine, exampleTail(consumed, 20))
    }

    /* at least: an earlier failed run may have left more on the queue; a command counting every envelope would take the three left over as its own */
    if 3 > strings.Count(consumed, exampleDispatchForeignLine) {
        fail("%s: messagebus:dispatch --timeout 5s printed fewer than three %q lines for the messages the --no-consume run left:\n%s", label, exampleDispatchForeignLine, exampleTail(consumed, 20))
    }

    pass("%s: messagebus:dispatch --no-consume printed three dispatched lines and no consumed one; the next run took the three it left as another producer's and printed %q", label, exampleDispatchSuccessLine)
}

/* removeExampleRecordsOf removes the journal entries and the audit trail a probe write left in the major's own tables */
func removeExampleRecordsOf(label string, major exampleMajor, mysqlDsn string, subject string, subjectId string) {
    if "" == mysqlDsn {
        return
    }

    database := openMysql(label, mysqlDsn)
    defer database.Close()

    removeExampleRecordsIn(label, database, major, subject, subjectId)
}

func removeExampleRecordsIn(label string, database *bun.DB, major exampleMajor, subject string, subjectId string) {
    removeExampleRowsOf(label, database, exampleJournalTable(major), "subject", "subject_id", subject, subjectId)
    removeExampleRowsOf(label, database, exampleAuditTable(major), "entity", "entity_id", subject, subjectId)
}

func exampleAuditTable(major exampleMajor) string {
    return "melody_example_" + major.label + "_audit"
}

func removeExampleRowsOf(label string, database *bun.DB, table string, subjectColumn string, idColumn string, subject string, subjectId string) {
    if false == exampleTableExists(label, database, table) {
        return
    }

    _, deleteErr := database.
        NewDelete().
        Table(table).
        Where("? = ?", bun.Ident(subjectColumn), subject).
        Where("? = ?", bun.Ident(idColumn), subjectId).
        Exec(context.Background())
    if nil != deleteErr {
        fail("%s: remove the rows of %s %s from %s: %v", label, subject, subjectId, table, deleteErr)
    }
}
