package main

import (
    "context"
    "encoding/json"
    "io"
    "net/http"
    "strings"
    "time"

    coderwebsocket "github.com/coder/websocket"
)

const catalogNotificationLabel = "catalog notification"

const (
    catalogNotificationProbeName  = "e2e notification probe"
    catalogNotificationRateLimit  = "melody-example-v3:rate_limit:"
    catalogNotificationReadWindow = 10 * time.Second
)

/* catalogNotificationPayload is what the application tells an open page when the nomenclature changes. */
type catalogNotificationPayload struct {
    Action    string `json:"action"`
    Subject   string `json:"subject"`
    SubjectId string `json:"subjectId"`
}

/* runCatalogNotificationCheck proves the socket carries the application's own changes to every open page.

   The WEBSOCKET FAN-OUT section already proves the handshake and the hub's fan-out, but it does it in process, against a hub it built itself and broadcast into itself. What it cannot show is that the RUNNING application broadcasts when its nomenclature changes — that the listener which invalidates the cache and writes the journal also tells the browsers, and that it does so through the socket a real page connects to.

   So: two live clients connect to the supervised application's /ws, one product is created through the API, and both clients must receive the same notification naming that product. Two clients rather than one because a broadcast that reached only the connection that happened to be first is not a fan-out, and a page opened in a second tab would silently stay stale. */
func runCatalogNotificationCheck(baseUrl string, mysqlDsn string, redisAddress string) {
    if "" == mysqlDsn {
        skip(
            "%s: MYSQL_DSN is cleared, so the probe product this section creates could not be removed with the records it leaves behind",
            catalogNotificationLabel,
        )

        return
    }

    if "" == redisAddress {
        skip(
            "%s: REDIS_ADDRESS is cleared, so the write budget an earlier section spent cannot be reset and the probe write would be refused",
            catalogNotificationLabel,
        )

        return
    }

    database := openMysql(catalogNotificationLabel, mysqlDsn)
    defer func() {
        _ = database.Close()
    }()

    removeExampleV3ProductProbes(catalogNotificationLabel, database, []string{catalogNotificationProbeName})
    defer removeExampleV3ProductProbes(catalogNotificationLabel, database, []string{catalogNotificationProbeName})

    dialCtx, dialCancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer dialCancel()

    socketUrl := strings.Replace(strings.TrimRight(baseUrl, "/"), "http", "ws", 1) + "/ws"

    first, _, firstDialErr := coderwebsocket.Dial(dialCtx, socketUrl, nil)
    if nil != firstDialErr {
        fail("%s: dial the first client on %s: %v", catalogNotificationLabel, socketUrl, firstDialErr)
    }
    defer first.Close(coderwebsocket.StatusNormalClosure, "")

    second, _, secondDialErr := coderwebsocket.Dial(dialCtx, socketUrl, nil)
    if nil != secondDialErr {
        fail("%s: dial the second client: %v", catalogNotificationLabel, secondDialErr)
    }
    defer second.Close(coderwebsocket.StatusNormalClosure, "")

    pass("two clients are connected to the running application's socket")

    resetExampleRateLimitCounters(catalogNotificationLabel, redisAddress, catalogNotificationRateLimit)

    client := newExampleHttpClient()
    signInExampleHttpEditor(client, baseUrl, "")

    productId := createCatalogNotificationProbe(client, baseUrl)

    firstNotification := readCatalogNotification(first, "first")
    secondNotification := readCatalogNotification(second, "second")

    for name, notification := range map[string]catalogNotificationPayload{"first": firstNotification, "second": secondNotification} {
        if productId != notification.SubjectId {
            fail(
                "%s: the %s client was told about %q while the section created %q — the notification does not name the change that caused it",
                catalogNotificationLabel,
                name,
                notification.SubjectId,
                productId,
            )
        }

        if "created" != notification.Action || "product" != notification.Subject {
            fail(
                "%s: the %s client was told %q on %q, wanted a created product",
                catalogNotificationLabel,
                name,
                notification.Action,
                notification.Subject,
            )
        }
    }

    pass(
        "both clients were told the same thing about %s (%s %s), so the write reached every open page rather than one of them",
        productId,
        firstNotification.Action,
        firstNotification.Subject,
    )
}

func createCatalogNotificationProbe(client *http.Client, baseUrl string) string {
    body := `{"name":"` + catalogNotificationProbeName + `","description":"probe","categoryId":"cat-1","price":5,"currencyId":"cur-eur","stock":1}`

    request, requestErr := http.NewRequest(
        "POST",
        strings.TrimRight(baseUrl, "/")+"/products/api/create/",
        strings.NewReader(body),
    )
    if nil != requestErr {
        fail("%s: build the create request: %v", catalogNotificationLabel, requestErr)
    }

    request.Header.Set("Content-Type", "application/json")
    request.Header.Set("Accept", "application/json")

    response, responseErr := client.Do(request)
    if nil != responseErr {
        fail("%s: create the probe: %v", catalogNotificationLabel, responseErr)
    }
    defer response.Body.Close()

    payload, readErr := io.ReadAll(response.Body)
    if nil != readErr {
        fail("%s: read the create response: %v", catalogNotificationLabel, readErr)
    }

    if http.StatusUnauthorized == response.StatusCode || http.StatusForbidden == response.StatusCode {
        fail("%s: the probe write was refused with %d — the section reached the firewall, not the nomenclature", catalogNotificationLabel, response.StatusCode)
    }

    if http.StatusTooManyRequests == response.StatusCode {
        fail("%s: the probe write was refused with 429 — the write budget was not reset before this section", catalogNotificationLabel)
    }

    if http.StatusOK != response.StatusCode && http.StatusCreated != response.StatusCode {
        fail("%s: the probe write was refused with %d: %s", catalogNotificationLabel, response.StatusCode, exampleTruncate(string(payload)))
    }

    created := exampleProduct{}
    decodeExampleData(catalogNotificationLabel, "the notification probe", string(payload), &created)

    if "" == created.Id {
        fail("%s: the probe write reported no identifier: %s", catalogNotificationLabel, exampleTruncate(string(payload)))
    }

    return created.Id
}

/* readCatalogNotification waits for the one frame the write must produce. A client that receives nothing is the failure this section exists to catch, so the wait is bounded and the timeout is named for what it means. */
func readCatalogNotification(connection *coderwebsocket.Conn, name string) catalogNotificationPayload {
    readCtx, readCancel := context.WithTimeout(context.Background(), catalogNotificationReadWindow)
    defer readCancel()

    _, frame, readErr := connection.Read(readCtx)
    if nil != readErr {
        fail(
            "%s: the %s client received nothing within %s of the write (%v) — the change was committed without telling the open pages",
            catalogNotificationLabel,
            name,
            catalogNotificationReadWindow,
            readErr,
        )
    }

    notification := catalogNotificationPayload{}
    if decodeErr := json.Unmarshal(frame, &notification); nil != decodeErr {
        fail(
            "%s: the %s client received an undecodable frame (%v): %s",
            catalogNotificationLabel,
            name,
            decodeErr,
            exampleTruncate(string(frame)),
        )
    }

    return notification
}
