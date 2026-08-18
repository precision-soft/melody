package main

import (
    "context"
    "fmt"
    "net/http"
    "os"
    "time"

    "github.com/precision-soft/melody/v3/security/totp"
)

const twoFactorLabel = "two-factor over http"

const (
    twoFactorEnrollRoute = "/twofactor/enroll"
    twoFactorVerifyRoute = "/twofactor/verify"
)

/* twoFactorTable and twoFactorPrimaryKeyColumn name the demo enrollment table (.example/twofactor/store.go). The
primary key is the USER IDENTIFIER, which is exactly why the section mints a fresh one per run: enrolling a fixed
identifier succeeds once and then collides with the row the previous run left behind, so the section would go red
on its second execution for a reason that has nothing to do with the code under test. */
const (
    twoFactorTable             = "melody_example_v3_two_factor"
    twoFactorPrimaryKeyColumn  = "user_identifier"
    twoFactorCodeHeader        = "X-2FA-Code"
    twoFactorExpiredPeriodBack = 5
)

/* twoFactorCollisionRetryCount is how many further periods the expired-code search may walk back when a candidate
happens to render the same six digits as the current code. */
const twoFactorCollisionRetryCount = 5

type twoFactorEnrollPayload struct {
    UserIdentifier string   `json:"userIdentifier"`
    Secret         string   `json:"secret"`
    OtpauthUri     string   `json:"otpauthUri"`
    RecoveryCodes  []string `json:"recoveryCodes"`
}

/* runTwoFactorCheck enrolls a throwaway user, verifies a code its own authenticator-app stand-in produced
(totp:code), and then attacks the verification four ways. The replay pair is the interesting one: an accepted TOTP
code stays arithmetically valid for its whole window, so the only thing stopping a captured code from being reused
inside that window is the replay guard — and the SECOND replay proves the guard keys on the NORMALIZED code, which
is what makes "409 643" fail to buy a second use of a code already spent as "409643". */
func runTwoFactorCheck(baseUrl string) {
    client := newLiveExampleClient(baseUrl)

    user := liveExampleUnique("e2e-two-factor")

    enrollment := assertTwoFactorEnrollment(client, user)

    assertTwoFactorNeverEnrolledFailsClosed(client)
    assertTwoFactorExpiredCodeRefused(client, user, enrollment.Secret)

    code := assertTwoFactorCodeAccepted(client, user, enrollment.Secret)

    assertTwoFactorVerifyRefused(client, user, code, "the same code replayed verbatim")
    assertTwoFactorVerifyRefused(client, user, twoFactorSpacedCode(code), "the same code replayed with whitespace inserted")

    cleanUpTwoFactorEnrollment(user)
}

func assertTwoFactorEnrollment(client *liveExampleClient, user string) twoFactorEnrollPayload {
    path := twoFactorEnrollRoute + "?user=" + user

    response := client.call(twoFactorLabel, liveExampleRequest{method: "POST", path: path})
    requireLiveExampleStatus(twoFactorLabel, path, response, http.StatusOK)

    payload := twoFactorEnrollPayload{}
    decodeLiveExamplePayload(twoFactorLabel, response, &payload)

    if user != payload.UserIdentifier {
        fail("%s: the enrollment echoed user %q, wanted %q", twoFactorLabel, payload.UserIdentifier, user)
    }
    if "" == payload.Secret {
        fail("%s: the enrollment returned an empty secret, so nothing below can produce a code", twoFactorLabel)
    }
    if 0 == len(payload.RecoveryCodes) {
        fail("%s: the enrollment returned no recovery codes", twoFactorLabel)
    }

    pass("enrolled the throwaway user %q with a secret and %d recovery codes", user, len(payload.RecoveryCodes))

    return payload
}

func assertTwoFactorCodeAccepted(client *liveExampleClient, user string, secret string) string {
    output, elapsed := runExampleMintCommand(twoFactorLabel, "totp:code", "--secret", secret)
    code := exampleMintedTotpCode(twoFactorLabel, output)

    response := twoFactorVerify(client, user, code)

    if http.StatusUnauthorized == response.statusCode {
        fail(
            "%s: the code the application's own totp:code command produced was refused with 401%s",
            twoFactorLabel,
            exampleMintDelayDiagnostic(elapsed),
        )
    }
    requireLiveExampleStatus(twoFactorLabel, twoFactorVerifyRoute+" with a fresh code", response, http.StatusOK)

    verified := struct {
        Factor   string `json:"factor"`
        Verified bool   `json:"verified"`
    }{}
    decodeLiveExamplePayload(twoFactorLabel, response, &verified)

    if "totp" != verified.Factor {
        fail("%s: the verification reports factor %q, wanted totp", twoFactorLabel, verified.Factor)
    }
    if false == verified.Verified {
        fail("%s: the verification answered 200 with verified=false", twoFactorLabel)
    }

    pass("the code totp:code produced for the stored secret was accepted (factor=%s)", verified.Factor)

    return code
}

func assertTwoFactorVerifyRefused(client *liveExampleClient, user string, code string, what string) {
    response := twoFactorVerify(client, user, code)

    if http.StatusUnauthorized != response.statusCode {
        fail(
            "%s: %s was answered %d, wanted 401 — a captured code buys a second use for the rest of its window: %s",
            twoFactorLabel,
            what,
            response.statusCode,
            exampleTruncate(response.bodyText()),
        )
    }

    pass("%s was refused with 401", what)
}

/* assertTwoFactorExpiredCodeRefused generates a code for a timestamp several periods in the past. The command has
no --at flag, so this is computed in-process through the same totp package the application verifies with, which is
what keeps the two from drifting apart on a config change. The generated code is checked against the current one
first: on the roughly one-in-a-million chance that they collide, the section would otherwise assert that a VALID
code is refused. */
func assertTwoFactorExpiredCodeRefused(client *liveExampleClient, user string, secret string) {
    expired, _, periodsBack, computeErr := twoFactorExpiredCodeAt(secret, time.Now())
    if nil != computeErr {
        fail("%s: compute an expired code: %v", twoFactorLabel, computeErr)
    }

    response := twoFactorVerify(client, user, expired)
    if http.StatusUnauthorized != response.statusCode {
        fail(
            "%s: a code from %d periods ago was answered %d, wanted 401 — the verification window is unbounded, so any code ever issued stays usable",
            twoFactorLabel,
            periodsBack,
            response.statusCode,
        )
    }

    pass("a code generated %d periods in the past was refused with 401 (the window is bounded)", periodsBack)
}

/* twoFactorExpiredCodeAt returns a code that sits outside the verification window at the reference instant, the code
that IS current there, and how many periods back it had to reach.

Two details make it worth its own function. The period comes from the totp package's own resolved config, so the
"N periods back" distance can never drift from the window Verify honours. And the candidate is compared against the
CURRENT code: a six-digit code repeats about once in a million periods, and on that collision the section would be
asserting that a perfectly valid code must be refused — a red run with no defect behind it. The search walks further
back until the two differ, and reports a failure rather than returning a colliding code. */
func twoFactorExpiredCodeAt(secret string, reference time.Time) (string, string, int, error) {
    resolved := totp.Config{}.Resolve()
    step := time.Duration(resolved.Period) * time.Second

    current, currentErr := totp.GenerateCodeAt(secret, reference, totp.Config{})
    if nil != currentErr {
        return "", "", 0, currentErr
    }

    for periodsBack := twoFactorExpiredPeriodBack; periodsBack <= twoFactorExpiredPeriodBack+twoFactorCollisionRetryCount; periodsBack++ {
        candidate, candidateErr := totp.GenerateCodeAt(secret, reference.Add(-time.Duration(periodsBack)*step), totp.Config{})
        if nil != candidateErr {
            return "", current, 0, candidateErr
        }

        if candidate != current {
            return candidate, current, periodsBack, nil
        }
    }

    return "", current, 0, fmt.Errorf(
        "every candidate code from %d to %d periods back equals the current one",
        twoFactorExpiredPeriodBack,
        twoFactorExpiredPeriodBack+twoFactorCollisionRetryCount,
    )
}

/* assertTwoFactorNeverEnrolledFailsClosed probes a user that was never enrolled. The failure it guards against is
fail-OPEN: a lookup that reported "no enrollment" and treated the absent second factor as satisfied would answer
200 and hand every un-enrolled account a free pass through the second factor. */
func assertTwoFactorNeverEnrolledFailsClosed(client *liveExampleClient) {
    user := liveExampleUnique("e2e-never-enrolled")

    response := twoFactorVerify(client, user, "123456")
    if http.StatusUnauthorized != response.statusCode {
        fail(
            "%s: verifying a code for the never-enrolled user %q answered %d, wanted 401 — an account with no second factor must not pass the second-factor check: %s",
            twoFactorLabel,
            user,
            response.statusCode,
            exampleTruncate(response.bodyText()),
        )
    }

    pass("a user that was never enrolled failed closed with 401")
}

func twoFactorVerify(client *liveExampleClient, user string, code string) liveExampleResponse {
    return client.call(twoFactorLabel, liveExampleRequest{
        method:     "POST",
        path:       twoFactorVerifyRoute + "?user=" + user,
        headerList: map[string]string{twoFactorCodeHeader: code},
    })
}

/* twoFactorSpacedCode splits a six-digit code the way an authenticator app displays it. Verification normalizes
before comparing, so this is the SAME code — which is precisely why the replay guard has to key on the normalized
form. */
func twoFactorSpacedCode(code string) string {
    if 6 != len(code) {
        return code
    }

    return code[:3] + " " + code[3:]
}

/* cleanUpTwoFactorEnrollment deletes the row the section created. The identifier is unique per run, so a leftover
row breaks nothing — but it accumulates one row per run forever in a shared dev database, so it is removed when the
harness has a connection of its own. When MYSQL_DSN is cleared the row is announced rather than left silent: a
reader who later finds unexplained rows in the demo table must be able to trace them to this section. */
func cleanUpTwoFactorEnrollment(user string) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        fmt.Printf(
            "NOTE  %s: MYSQL_DSN is not set, so the enrollment row %s=%q was LEFT BEHIND in %s\n",
            twoFactorLabel,
            twoFactorPrimaryKeyColumn,
            user,
            twoFactorTable,
        )

        return
    }

    database := openMysql(twoFactorLabel, dsn)
    defer func() {
        _ = database.Close()
    }()

    query := fmt.Sprintf("DELETE FROM %s WHERE %s = ?", twoFactorTable, twoFactorPrimaryKeyColumn)

    if _, deleteErr := database.ExecContext(context.Background(), query, user); nil != deleteErr {
        fail("%s: delete the enrollment row for %q: %v", twoFactorLabel, user, deleteErr)
    }

    pass("removed the throwaway enrollment row for %q from %s", user, twoFactorTable)
}
