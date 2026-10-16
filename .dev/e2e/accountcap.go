package main

import (
    "io"
    "net/http"
    "os"
    "strings"
    "time"
)

const accountCapLabel = "account caps"

/* exampleSessionCap and the two stream caps are the example's own (repository.UserSessionCap, event.StreamCapPerUser) */
const (
    exampleSessionCap       = 5
    exampleStreamCapPerUser = 4
)

/* assertAccountCapsHold drives the two caps one account is held to, over an account of its own so no other section's session or stream counts against it. The sign-in past the session cap ends the account's oldest session, whose cookie then reads as signed out while the newer ones still answer; the event streams the account holds open are capped across the stream door and the websocket route alike, a stream past the cap answered 429 before anything is committed, and a closed stream gives its place back. The account leaves through the admin door with its audit trail, on a failure too. */
func assertAccountCapsHold(administrator *http.Client, baseUrl string) {
    mysqlDsn := os.Getenv("MYSQL_DSN")
    if "" == mysqlDsn {
        skip("%s: MYSQL_DSN is cleared, so the probe account's audit trail could not be removed with it — the caps were NOT checked", accountCapLabel)

        return
    }

    database := openMysql(accountCapLabel, mysqlDsn)
    defer database.Close()

    username := liveExampleUnique("cap-probe")
    password := "cap-probe-password"

    output := runExampleMintCommandWithInput(accountCapLabel, password+"\n", "example:user:create", "--role", "ROLE_EDITOR", username)
    match := exampleCreatedUserPattern.FindStringSubmatch(output)
    if 2 != len(match) {
        fail("%s: example:user:create printed no created account: %s", accountCapLabel, exampleTail(output, 5))
    }

    userId := match[1]

    removeOnFailure := pushFailureCleanup(func() {
        deleteExampleAccount(administrator, baseUrl, userId)
        removeExampleV3AuditTrail(accountCapLabel, database, "user", userId)
    })
    defer removeOnFailure()

    clientList := make([]*http.Client, 0, exampleSessionCap+1)
    for range exampleSessionCap + 1 {
        client := newExampleHttpClient()
        signInExampleHttp(client, baseUrl, "", username, password)
        clientList = append(clientList, client)
    }

    if status := accountCapProbeStatus(clientList[0], baseUrl); http.StatusOK == status {
        fail("%s: the oldest of %d sessions still answers 200 after the sign-in past the cap", accountCapLabel, exampleSessionCap+1)
    }

    for index, client := range clientList[1:] {
        if status := accountCapProbeStatus(client, baseUrl); http.StatusOK != status {
            fail("%s: session %d of the newest %d answered %d, wanted 200", accountCapLabel, index+2, exampleSessionCap, status)
        }
    }

    pass("the sign-in past %d sessions ended the account's oldest one and kept the newer %d", exampleSessionCap, exampleSessionCap)

    assertAccountStreamCapHolds(exampleSessionCookieHeader(clientList[len(clientList)-1], baseUrl), baseUrl)

    deleteExampleAccount(administrator, baseUrl, userId)
    removeExampleV3AuditTrail(accountCapLabel, database, "user", userId)
}

/* assertAccountStreamCapHolds holds the account's whole stream allowance open, then asks for one more on each door: the stream door and the websocket upgrade both answer 429 from the one count, and a stream that closes frees the place for the next */
func assertAccountStreamCapHolds(sessionCookieHeader string, baseUrl string) {
    client := newLiveExampleClient(baseUrl)
    headerList := map[string]string{"Accept": "text/event-stream", "Cookie": sessionCookieHeader}
    streamPath := serverSentEventStreamRoute + "?topic=" + liveExampleUnique("e2e-cap")

    heldList := make([]*http.Response, 0, exampleStreamCapPerUser)
    releaseStreams := pushFailureCleanup(func() {
        for _, held := range heldList {
            _ = held.Body.Close()
        }
    })
    defer releaseStreams()

    for index := range exampleStreamCapPerUser {
        held := client.openStream(accountCapLabel, streamPath, headerList)
        heldList = append(heldList, held)

        if http.StatusOK != held.StatusCode {
            fail("%s: stream %d of %d answered %d, wanted 200", accountCapLabel, index+1, exampleStreamCapPerUser, held.StatusCode)
        }
    }

    pastCap := client.openStream(accountCapLabel, streamPath, headerList)
    _, _ = io.Copy(io.Discard, pastCap.Body)
    _ = pastCap.Body.Close()
    if http.StatusTooManyRequests != pastCap.StatusCode {
        fail("%s: the stream past %d answered %d, wanted 429", accountCapLabel, exampleStreamCapPerUser, pastCap.StatusCode)
    }

    upgrade := client.openStream(accountCapLabel, "/ws", map[string]string{
        "Cookie":                sessionCookieHeader,
        "Connection":            "Upgrade",
        "Upgrade":               "websocket",
        "Sec-WebSocket-Version": "13",
        "Sec-WebSocket-Key":     "dGhlIHNhbXBsZSBub25jZQ==",
        "Origin":                strings.TrimRight(baseUrl, "/"),
    })
    _ = upgrade.Body.Close()
    if http.StatusTooManyRequests != upgrade.StatusCode {
        fail("%s: the websocket upgrade past the stream cap answered %d, wanted 429", accountCapLabel, upgrade.StatusCode)
    }

    _ = heldList[0].Body.Close()
    heldList = heldList[1:]

    /* the handler gives the slot back once it sees the client gone, on its next write or the request context's end, so the next stream is retried for a moment */
    admitted := false
    for range 50 {
        next := client.openStream(accountCapLabel, streamPath, headerList)
        if http.StatusOK == next.StatusCode {
            heldList = append(heldList, next)
            admitted = true

            break
        }

        _, _ = io.Copy(io.Discard, next.Body)
        _ = next.Body.Close()
        time.Sleep(100 * time.Millisecond)
    }

    if false == admitted {
        fail("%s: a closed stream did not give its place back", accountCapLabel)
    }

    pass("%d streams held: the next stream and the websocket upgrade answered 429 from one count, and a closed stream gave its place back", exampleStreamCapPerUser)
}

/* accountCapProbeStatus asks a route behind the session firewall what the client's session is worth */
func accountCapProbeStatus(client *http.Client, baseUrl string) int {
    request, requestErr := http.NewRequest("GET", strings.TrimRight(baseUrl, "/")+"/currencies/api/read/", nil)
    if nil != requestErr {
        fail("%s: build the probe request: %v", accountCapLabel, requestErr)
    }

    request.Header.Set("Accept", "application/json")

    response, responseErr := client.Do(request)
    if nil != responseErr {
        fail("%s: probe the session: %v", accountCapLabel, responseErr)
    }
    defer response.Body.Close()

    _, _ = io.Copy(io.Discard, response.Body)

    return response.StatusCode
}
