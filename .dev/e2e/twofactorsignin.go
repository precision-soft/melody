package main

import (
    "bufio"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "net/http/cookiejar"
    "net/url"
    "os"
    "strings"
    "time"

    "github.com/precision-soft/melody/v3/security/totp"
)

const (
    twoFactorLoginRoute = "/login/"

    /* twoFactorReplayKeyPrefix and twoFactorBudgetKeyPrefix are the redis names the example keeps the burned codes and the per-account budget under (.example/config/two_factor.go) */
    twoFactorReplayKeyPrefix = "melody-example-v3:2fa"
    twoFactorBudgetKeyPrefix = "melody-example-v3:second_factor_budget:"

    /* twoFactorBudgetAllowance is the example's secondFactorBudgetAllowance */
    twoFactorBudgetAllowance = 5

    /* twoFactorJournalPath and twoFactorSessionPath are the supervised example's journal and session file, read from the harness's own seat in the dev container */
    twoFactorJournalPath = "../../v3/.example/var/log/dev.log"
    twoFactorSessionPath = "../../v3/.example/var/session/session.json"
)

/* assertTwoFactorSignIn signs the enrolled editor in through the login door itself, where the second factor now gates the session rather than a second door after it. The password alone answers the challenge and is announced to nobody; the code the verification door already accepted is refused here, since both doors burn a code in one memory; a fresh code opens a session; the same code again is a failed sign-in; a recovery code opens one session once. The journal is read out of band: the challenge leaves no login failure line under its request id, while the refused replay leaves exactly one. The per-account budget closes the section: once the account has presented its allowance of codes, even a right one is refused 429 before it is read. */
func assertTwoFactorSignIn(baseUrl string, redisAddress string, enrollment twoFactorEnrollPayload, spentCode string) {
    challenge := twoFactorSignIn(baseUrl, nil)
    requireLiveExampleStatus(twoFactorLabel, twoFactorLoginRoute+" with the password alone", challenge.response, http.StatusUnauthorized)
    if false == strings.Contains(challenge.response.bodyText(), `"factor":"totp"`) || false == strings.Contains(challenge.response.bodyText(), "second factor required") {
        fail("%s: the enrolled editor's password alone was answered %s, wanted the challenge naming the totp factor", twoFactorLabel, exampleTruncate(challenge.response.bodyText()))
    }
    pass("the enrolled editor's password alone was answered 401 with the totp challenge, not a session")

    /* an expired code is refused 401 too, so the refusal proves the shared memory only while the spent code is still valid on both sides of the request */
    requireTwoFactorCodeStillValid(enrollment.Secret, spentCode, "before")
    crossDoor := twoFactorSignIn(baseUrl, map[string]string{twoFactorCodeHeader: spentCode})
    requireLiveExampleStatus(twoFactorLabel, twoFactorLoginRoute+" with the code the verification door accepted", crossDoor.response, http.StatusUnauthorized)
    requireTwoFactorCodeStillValid(enrollment.Secret, spentCode, "after")
    assertTwoFactorReplayKeyHeld(redisAddress, spentCode)
    pass("the code the verification door had accepted was refused at the login door while still valid, one memory of burned codes behind both")

    code := twoFactorFreshCode(enrollment.Secret, spentCode)

    accepted := twoFactorSignIn(baseUrl, map[string]string{twoFactorCodeHeader: code})
    requireLiveExampleStatus(twoFactorLabel, twoFactorLoginRoute+" with a fresh code", accepted.response, http.StatusOK)

    signedIn := accepted.client.call(twoFactorLabel, liveExampleRequest{method: "POST", path: twoFactorVerifyRoute})
    requireLiveExampleStatus(twoFactorLabel, twoFactorVerifyRoute+" with no factor on the session the code opened", signedIn, http.StatusBadRequest)

    assertTwoFactorSessionNames(accepted.client, enrollment.UserIdentifier)
    pass("a fresh code opened a session the firewall reads as signed in, stored under its cookie as %q", enrollment.UserIdentifier)

    replay := twoFactorSignIn(baseUrl, map[string]string{twoFactorCodeHeader: code})
    requireLiveExampleStatus(twoFactorLabel, twoFactorLoginRoute+" with the same code again", replay.response, http.StatusUnauthorized)
    if true == strings.Contains(replay.response.bodyText(), "second factor") {
        fail("%s: the refused replay names the factor it failed: %s", twoFactorLabel, exampleTruncate(replay.response.bodyText()))
    }
    pass("the same code presented again was refused 401 invalid credentials")

    assertTwoFactorReplayKeyHeld(redisAddress, code)

    recoveryHeader := map[string]string{twoFactorRecoveryHeader: enrollment.RecoveryCodes[1]}

    firstRecovery := twoFactorSignIn(baseUrl, recoveryHeader)
    requireLiveExampleStatus(twoFactorLabel, twoFactorLoginRoute+" with an unused recovery code", firstRecovery.response, http.StatusOK)

    secondRecovery := twoFactorSignIn(baseUrl, recoveryHeader)
    requireLiveExampleStatus(twoFactorLabel, twoFactorLoginRoute+" with the spent recovery code", secondRecovery.response, http.StatusUnauthorized)
    pass("a recovery code signed the editor in once at the login door and was refused the second time")

    retypedRecovery := strings.ToUpper(strings.ReplaceAll(enrollment.RecoveryCodes[2], "-", ""))
    retyped := twoFactorSignIn(baseUrl, map[string]string{twoFactorRecoveryHeader: retypedRecovery})
    requireLiveExampleStatus(twoFactorLabel, twoFactorLoginRoute+" with a recovery code retyped in upper case without its hyphen", retyped.response, http.StatusOK)
    pass("a recovery code retyped in upper case and without its hyphen signed the editor in, compared in its canonical form")

    assertTwoFactorSignInJournal(challenge.requestId(), replay.requestId())

    assertTwoFactorBudget(baseUrl, redisAddress, enrollment)
}

type twoFactorSignInAnswer struct {
    client   *liveExampleClient
    response liveExampleResponse
}

func (instance twoFactorSignInAnswer) requestId() string {
    return instance.response.headerList.Get("X-Request-Id")
}

/* twoFactorSignIn signs the editor in on a fresh client, carrying the second-factor headers given, as the login form posts */
func twoFactorSignIn(baseUrl string, headerList map[string]string) twoFactorSignInAnswer {
    jar, jarErr := cookiejar.New(nil)
    if nil != jarErr {
        fail("%s: build a cookie jar: %v", twoFactorLabel, jarErr)
    }

    client := &liveExampleClient{
        baseUrl: strings.TrimRight(baseUrl, "/"),
        client:  &http.Client{Jar: jar, Timeout: 15 * time.Second},
    }

    requestHeaders := map[string]string{"Accept": "application/json"}
    for name, value := range headerList {
        requestHeaders[name] = value
    }

    response := client.call(twoFactorLabel, liveExampleRequest{
        method:      "POST",
        path:        twoFactorLoginRoute,
        contentType: "application/json",
        headerList:  requestHeaders,
        body:        []byte(exampleCredentialBody(exampleHttpEditorUsername, exampleHttpEditorPassword)),
    })

    return twoFactorSignInAnswer{client: client, response: response}
}

/* twoFactorFreshCode waits into the next period when the current code is the one already spent, so the sign-in presents a code no door has burned */
func twoFactorFreshCode(secret string, spentCode string) string {
    deadline := time.Now().Add(40 * time.Second)

    for time.Now().Before(deadline) {
        code, codeErr := totp.GenerateCodeAt(secret, time.Now(), totp.Config{})
        if nil != codeErr {
            fail("%s: compute a code for the enrolled secret: %v", twoFactorLabel, codeErr)
        }

        if code != spentCode {
            return code
        }

        time.Sleep(time.Second)
    }

    fail("%s: no code other than the spent one within 40 seconds", twoFactorLabel)

    return ""
}

/* requireTwoFactorCodeStillValid fails the section when the spent code is no longer valid at this instant, where a refusal of it would read the same as the burned memory's */
func requireTwoFactorCodeStillValid(secret string, code string, moment string) {
    valid, verifyErr := totp.VerifyAt(secret, code, time.Now(), totp.Config{})
    if nil != verifyErr {
        fail("%s: verify the spent code %s the replay: %v", twoFactorLabel, moment, verifyErr)
    }

    if false == valid {
        fail("%s: the spent code was no longer valid %s the cross-door replay, so its refusal could not tell the shared memory from the expiry", twoFactorLabel, moment)
    }
}

/* assertTwoFactorReplayKeyHeld reads the burned code in redis under the example's prefix: the replay is refused by the shared memory, not by a guard held in one process */
func assertTwoFactorReplayKeyHeld(redisAddress string, code string) {
    client := openRedis(redisAddress)
    defer client.Close()

    keys, keysErr := client.Do(context.Background(), client.B().Keys().Pattern(twoFactorReplayKeyPrefix+"*").Build()).AsStrSlice()
    if nil != keysErr {
        fail("%s: list the burned codes: %v", twoFactorLabel, keysErr)
    }

    for _, key := range keys {
        if true == strings.HasSuffix(key, ":"+code) {
            pass("the accepted code is held in redis under %s", key)

            return
        }
    }

    fail("%s: no key under %s names the accepted code %s (found %v), so the replay was not refused by the shared memory", twoFactorLabel, twoFactorReplayKeyPrefix, code, keys)
}

/* assertTwoFactorSignInJournal reads the supervised example's journal: each request's access line proves the journal is the live one, the challenge must have written no login failure under its request id and the refused replay exactly one */
func assertTwoFactorSignInJournal(challengeRequestId string, replayRequestId string) {
    if "" == challengeRequestId || "" == replayRequestId {
        fail("%s: a sign-in answered no X-Request-Id (challenge %q, replay %q)", twoFactorLabel, challengeRequestId, replayRequestId)
    }

    deadline := time.Now().Add(5 * time.Second)

    for {
        accessLines, failureLines := twoFactorJournalCounts(challengeRequestId, replayRequestId)

        if 2 == accessLines {
            if 0 != failureLines[challengeRequestId] {
                fail("%s: the challenge wrote %d security login failure lines under %s, wanted none", twoFactorLabel, failureLines[challengeRequestId], challengeRequestId)
            }

            if 1 != failureLines[replayRequestId] {
                fail("%s: the refused replay wrote %d security login failure lines under %s, wanted one", twoFactorLabel, failureLines[replayRequestId], replayRequestId)
            }

            pass("out of band, the journal holds no login failure for the challenge and one for the refused replay")

            return
        }

        if time.Now().After(deadline) {
            fail("%s: the journal %s holds %d of the two access lines, so it is not the supervised example's live journal", twoFactorLabel, twoFactorJournalPath, accessLines)
        }

        time.Sleep(250 * time.Millisecond)
    }
}

func twoFactorJournalCounts(challengeRequestId string, replayRequestId string) (int, map[string]int) {
    journal, openErr := os.Open(twoFactorJournalPath)
    if nil != openErr {
        fail("%s: open the supervised example's journal: %v", twoFactorLabel, openErr)
    }
    defer journal.Close()

    accessLines := 0
    failureLines := map[string]int{}

    scanner := bufio.NewScanner(journal)
    scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

    for scanner.Scan() {
        line := scanner.Text()

        for _, requestId := range []string{challengeRequestId, replayRequestId} {
            if false == strings.Contains(line, `"requestId":"`+requestId+`"`) {
                continue
            }

            if true == strings.Contains(line, `"message":"request completed"`) {
                accessLines++
            }

            if true == strings.Contains(line, `"message":"security login failure"`) {
                failureLines[requestId]++
            }
        }
    }

    if scanErr := scanner.Err(); nil != scanErr {
        fail("%s: read the supervised example's journal: %v", twoFactorLabel, scanErr)
    }

    return accessLines, failureLines
}

/* assertTwoFactorBudget spends the editor's budget with wrong codes and then presents a right one, which must be refused 429 before it is read; the budget key is read in redis and removed, so the account signs in again after the section */
func assertTwoFactorBudget(baseUrl string, redisAddress string, enrollment twoFactorEnrollPayload) {
    resetExampleRateLimitCounters(twoFactorLabel, redisAddress, twoFactorBudgetKeyPrefix)

    for attempt := 0; attempt < twoFactorBudgetAllowance; attempt++ {
        refused := twoFactorSignIn(baseUrl, map[string]string{twoFactorCodeHeader: "000000"})
        requireLiveExampleStatus(twoFactorLabel, fmt.Sprintf("%s with a wrong code, attempt %d", twoFactorLoginRoute, attempt+1), refused.response, http.StatusUnauthorized)
    }

    code, codeErr := totp.GenerateCodeAt(enrollment.Secret, time.Now(), totp.Config{})
    if nil != codeErr {
        fail("%s: compute a code for the enrolled secret: %v", twoFactorLabel, codeErr)
    }

    spent := twoFactorSignIn(baseUrl, map[string]string{twoFactorCodeHeader: code})
    requireLiveExampleStatus(twoFactorLabel, twoFactorLoginRoute+" with a right code past the budget", spent.response, http.StatusTooManyRequests)

    client := openRedis(redisAddress)
    defer client.Close()

    budgetKey := twoFactorBudgetKeyPrefix + "account:" + enrollment.UserIdentifier
    exists, existsErr := client.Do(context.Background(), client.B().Exists().Key(budgetKey).Build()).AsInt64()
    if nil != existsErr || 1 != exists {
        fail("%s: the budget key %s is absent from redis (%v), so the 429 was not the per-account budget's", twoFactorLabel, budgetKey, existsErr)
    }

    resetExampleRateLimitCounters(twoFactorLabel, redisAddress, twoFactorBudgetKeyPrefix)

    pass("after %d wrong codes the editor's right code was refused 429 before it was read; the budget is held in redis under %s", twoFactorBudgetAllowance, budgetKey)
}

/* assertTwoFactorSessionNames reads the session the client's cookie names out of the example's session file: the sign-in wrote the enrolled account's identity under the id the response emitted. The file is saved as the response completes, so it is polled briefly. */
func assertTwoFactorSessionNames(client *liveExampleClient, userIdentifier string) {
    baseUrl, parseErr := url.Parse(client.baseUrl)
    if nil != parseErr {
        fail("%s: parse the base url: %v", twoFactorLabel, parseErr)
    }

    sessionId := ""
    for _, cookie := range client.client.Jar.Cookies(baseUrl) {
        if "MELODYSESSID" == cookie.Name {
            sessionId = cookie.Value
        }
    }
    if "" == sessionId {
        fail("%s: the sign-in left no session cookie on the client", twoFactorLabel)
    }

    deadline := time.Now().Add(5 * time.Second)

    for {
        content, readErr := os.ReadFile(twoFactorSessionPath)
        if nil != readErr {
            fail("%s: read the example's session file: %v", twoFactorLabel, readErr)
        }

        sessions := map[string]struct {
            Data map[string]any `json:"data"`
        }{}
        if decodeErr := json.Unmarshal(content, &sessions); nil != decodeErr {
            fail("%s: decode the example's session file: %v", twoFactorLabel, decodeErr)
        }

        if stored, exists := sessions[sessionId]; true == exists {
            if userIdentifier != stored.Data["security.userId"] {
                fail("%s: the session %s names %v, wanted %q", twoFactorLabel, sessionId, stored.Data["security.userId"], userIdentifier)
            }

            return
        }

        if time.Now().After(deadline) {
            fail("%s: the example's session file holds no session %s", twoFactorLabel, sessionId)
        }

        time.Sleep(250 * time.Millisecond)
    }
}
