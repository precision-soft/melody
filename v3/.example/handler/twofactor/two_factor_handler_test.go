package twofactor

import (
    "encoding/json"
    "errors"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    store2fa "github.com/precision-soft/melody/v3/.example/twofactor"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    "github.com/precision-soft/melody/v3/security/totp"
)

/* the account is the token's, so a caller without a token has nothing to name and is refused before anything is written. The `user` query parameter is sent anyway, because the assertion that matters is that it is not read: a handler that fell back to it would answer something other than 401. The store is nil on purpose: a nil handle that would panic if reached asserts that the guard stands first. */
func TestEnrollHandlerRefusesACallerWithoutAToken(t *testing.T) {
    request, runtimeInstance := twoFactorRequest(t, "/twofactor/enroll?user=admin")

    response, handlerErr := EnrollHandler(nil, nil)(runtimeInstance, httptest.NewRecorder(), request)
    assertTwoFactorUnauthorized(t, "the enrollment door", response, handlerErr)
}

func TestVerifyHandlerRefusesACallerWithoutAToken(t *testing.T) {
    request, runtimeInstance := twoFactorRequest(t, "/twofactor/verify?user=admin")

    response, handlerErr := VerifyHandler(nil, nil)(runtimeInstance, httptest.NewRecorder(), request)
    assertTwoFactorUnauthorized(t, "the verification door", response, handlerErr)
}

func assertTwoFactorUnauthorized(t *testing.T, door string, response melodyhttpcontract.Response, handlerErr error) {
    t.Helper()

    if nil != handlerErr {
        t.Fatalf("expected %s to answer the refusal itself, got %v", door, handlerErr)
    }

    if nil == response {
        t.Fatalf("expected %s to refuse a caller with no token", door)
    }

    if nethttp.StatusUnauthorized != response.StatusCode() {
        t.Fatalf("expected %s to refuse with 401, got %d", door, response.StatusCode())
    }
}

/* the account the enrollment writes is the token's, whatever the request names: the row a second enrollment replaces is the caller's own only while no name the request carries reaches the store */
func TestEnrollHandlerWritesTheTokensRowWhateverTheRequestNames(t *testing.T) {
    store, connector := enrolledStore(t, "", "")
    request, runtimeInstance := authenticatedTwoFactorRequest(t, "/twofactor/enroll?user=admin", "alice", nil)

    response, handlerErr := EnrollHandler(fixedStore(store), nil)(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr || nil == response || nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected the enrollment to answer 200, got %v, %v", response, handlerErr)
    }

    statements := connector.recorded()
    insertList := insertStatements(statements)
    if 1 != len(insertList) {
        t.Fatalf("expected the enrollment to write one row, got %q", statements)
    }

    for _, statement := range statements {
        if false == strings.Contains(statement, "'alice'") || true == strings.Contains(statement, "'admin'") {
            t.Fatalf("expected the enrollment to read and write the token's row and never the one the request named, got %q", statement)
        }
    }

    body, readErr := io.ReadAll(response.BodyReader())
    if nil != readErr {
        t.Fatalf("reading the enrollment's answer failed: %v", readErr)
    }

    var envelope struct {
        Payload struct {
            UserIdentifier string `json:"userIdentifier"`
        } `json:"payload"`
    }
    if decodeErr := json.Unmarshal(body, &envelope); nil != decodeErr || "alice" != envelope.Payload.UserIdentifier {
        t.Fatalf("expected the enrollment to answer for the token's account, got %s (%v)", body, decodeErr)
    }
}

/* the verification reads the token's enrollment: only alice is enrolled here, so a door that read the account the request named would find none and refuse a code alice's own secret produces */
func TestVerifyHandlerChecksTheTokensEnrollmentWhateverTheRequestNames(t *testing.T) {
    secret, secretErr := totp.GenerateSecret()
    if nil != secretErr {
        t.Fatalf("generating the fixture secret failed: %v", secretErr)
    }

    code, codeErr := totp.GenerateCodeAt(secret, time.Now(), totp.Config{})
    if nil != codeErr {
        t.Fatalf("generating the fixture code failed: %v", codeErr)
    }

    store, connector := enrolledStore(t, "alice", secret)
    request, runtimeInstance := authenticatedTwoFactorRequest(
        t,
        "/twofactor/verify?user=admin",
        "alice",
        map[string]string{melodysecurity.DefaultTotpCodeHeaderName: code},
    )

    response, handlerErr := VerifyHandler(fixedStore(store), nil)(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr || nil == response || nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected the token's own code to verify with 200, got %v, %v, reads %q", response, handlerErr, connector.recorded())
    }

    for _, statement := range connector.recorded() {
        if true == strings.Contains(statement, "'admin'") {
            t.Fatalf("expected the verification never to read the account the request named, got %q", statement)
        }
    }
}

/* the recovery door redeems against the token's enrollment too */
func TestVerifyHandlerRedeemsAgainstTheTokensEnrollmentWhateverTheRequestNames(t *testing.T) {
    store, connector := enrolledStore(t, "alice", "JBSWY3DPEHPK3PXP")
    request, runtimeInstance := authenticatedTwoFactorRequest(
        t,
        "/twofactor/verify?user=admin",
        "alice",
        map[string]string{melodysecurity.DefaultTotpRecoveryHeaderName: "recovery-one"},
    )

    response, handlerErr := VerifyHandler(fixedStore(store), nil)(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr || nil == response || nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected the token's own recovery code to be redeemed with 200, got %v, %v, statements %q", response, handlerErr, connector.recorded())
    }

    statements := connector.recorded()

    for _, statement := range statements {
        if true == strings.Contains(statement, "'admin'") {
            t.Fatalf("expected the recovery door never to reach the account the request named, got %q", statement)
        }
    }
}

/* healingStore answers the given refusal on its first call and the store on every later one: the source a
   door meets while the store's migration is refused and after it heals, inside one process */
func healingStore(store *store2fa.Store, refusal error) (store2fa.StoreSource, *int) {
    calls := 0

    return func(runtimeInstance melodyruntimecontract.Runtime) (*store2fa.Store, error) {
        calls = calls + 1
        if 1 == calls {
            return nil, refusal
        }

        return store, nil
    }, &calls
}

/* the store is resolved at each request, so a refusal is the request's and not the process's: the door that met it answers 503 without a statement reaching the database, and the next request in the same process, the cause gone, enrolls. */
func TestEnrollHandlerAnswersAStoreRefusalWith503AndEnrollsOnceItHeals(t *testing.T) {
    store, connector := enrolledStore(t, "", "")
    storeSource, calls := healingStore(store, errors.New("the catalogue database refused the migration"))
    handler := EnrollHandler(storeSource, nil)

    request, runtimeInstance := authenticatedTwoFactorRequest(t, "/twofactor/enroll", "alice", nil)
    response, handlerErr := handler(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr || nil == response || nethttp.StatusServiceUnavailable != response.StatusCode() {
        t.Fatalf("expected a refused store to answer 503, got %v, %v", response, handlerErr)
    }

    if 0 != len(connector.recorded()) {
        t.Fatalf("expected no statement while the store is refused, got %q", connector.recorded())
    }

    request, runtimeInstance = authenticatedTwoFactorRequest(t, "/twofactor/enroll", "alice", nil)
    response, handlerErr = handler(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr || nil == response || nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected the next request to enroll once the store resolves, got %v, %v", response, handlerErr)
    }

    if 2 != *calls || 1 != len(insertStatements(connector.recorded())) {
        t.Fatalf("expected the store asked once per request and one row written, got %d asks and %q", *calls, connector.recorded())
    }
}

/* the verification door resolves the same way: 503 while the store is refused, and the code alice's own
   secret produces verifies at the next request */
func TestVerifyHandlerAnswersAStoreRefusalWith503AndVerifiesOnceItHeals(t *testing.T) {
    secret, secretErr := totp.GenerateSecret()
    if nil != secretErr {
        t.Fatalf("generating the fixture secret failed: %v", secretErr)
    }

    code, codeErr := totp.GenerateCodeAt(secret, time.Now(), totp.Config{})
    if nil != codeErr {
        t.Fatalf("generating the fixture code failed: %v", codeErr)
    }

    store, _ := enrolledStore(t, "alice", secret)
    storeSource, _ := healingStore(store, errors.New("the catalogue database refused the migration"))
    handler := VerifyHandler(storeSource, nil)

    headers := map[string]string{melodysecurity.DefaultTotpCodeHeaderName: code}

    request, runtimeInstance := authenticatedTwoFactorRequest(t, "/twofactor/verify", "alice", headers)
    response, handlerErr := handler(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr || nil == response || nethttp.StatusServiceUnavailable != response.StatusCode() {
        t.Fatalf("expected a refused store to answer 503, got %v, %v", response, handlerErr)
    }

    request, runtimeInstance = authenticatedTwoFactorRequest(t, "/twofactor/verify", "alice", headers)
    response, handlerErr = handler(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr || nil == response || nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected the next request to verify once the store resolves, got %v, %v", response, handlerErr)
    }
}

/* a code the sign-in already burned in the shared guard is refused here: the door writes the nonce the framework's second-factor authenticator writes, so the code is spent once across both doors */
func TestVerifyHandlerRefusesACodeTheSignInAlreadyBurnedInTheSharedGuard(t *testing.T) {
    secret, secretErr := totp.GenerateSecret()
    if nil != secretErr {
        t.Fatalf("generating the fixture secret failed: %v", secretErr)
    }

    code, codeErr := totp.GenerateCodeAt(secret, time.Now(), totp.Config{})
    if nil != codeErr {
        t.Fatalf("generating the fixture code failed: %v", codeErr)
    }

    store, _ := enrolledStore(t, "alice", secret)
    request, runtimeInstance := authenticatedTwoFactorRequest(
        t,
        "/twofactor/verify",
        "alice",
        map[string]string{melodysecurity.DefaultTotpCodeHeaderName: code},
    )

    sharedGuard := melodysecurity.NewMemoryNonceGuard()
    if seen, rememberErr := sharedGuard.Remember(runtimeInstance, "2fa:alice:"+totp.NormalizeCode(code), time.Minute); nil != rememberErr || true == seen {
        t.Fatalf("burning the code as the sign-in does failed: %v %v", seen, rememberErr)
    }

    response, handlerErr := VerifyHandler(fixedStore(store), sharedGuard)(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr || nil == response || nethttp.StatusUnauthorized != response.StatusCode() {
        t.Fatalf("expected a code the shared guard holds refused with 401, got %v, %v", response, handlerErr)
    }
}

/* replacing an enrollment asks for a factor of the one it replaces: a session alone is answered with the challenge and the stored secret is not written, a wrong code is refused the same way, and neither spends what the replay guard holds */
func TestEnrollHandlerRefusesToReplaceAnEnrollmentWithoutACurrentFactor(t *testing.T) {
    secret, secretErr := totp.GenerateSecret()
    if nil != secretErr {
        t.Fatalf("generating the fixture secret failed: %v", secretErr)
    }

    for _, headers := range []map[string]string{
        nil,
        {melodysecurity.DefaultTotpCodeHeaderName: "000000"},
        {melodysecurity.DefaultTotpRecoveryHeaderName: "not-a-recovery-code"},
    } {
        store, connector := enrolledStore(t, "alice", secret)
        request, runtimeInstance := authenticatedTwoFactorRequest(t, "/twofactor/enroll", "alice", headers)

        response, handlerErr := EnrollHandler(fixedStore(store), nil)(runtimeInstance, httptest.NewRecorder(), request)
        if nil != handlerErr || nil == response || nethttp.StatusUnauthorized != response.StatusCode() {
            t.Fatalf("%v: expected a replacement without a current factor refused with 401, got %v, %v", headers, response, handlerErr)
        }

        if 0 != len(insertStatements(connector.recorded())) {
            t.Fatalf("%v: expected the stored secret left unwritten, got %q", headers, connector.recorded())
        }

        if nil == headers {
            body, _ := io.ReadAll(response.BodyReader())
            if false == strings.Contains(string(body), `"factor":"totp"`) || false == strings.Contains(string(body), "second factor required") {
                t.Fatalf("expected the challenge naming the factor, got %s", body)
            }
        }
    }
}

/* a current TOTP code replaces the enrollment and is burned in the guard the sign-in and the verification door share: the same code is refused at the verification door afterwards */
func TestEnrollHandlerReplacesWithACurrentCodeAndBurnsItAcrossTheDoors(t *testing.T) {
    secret, secretErr := totp.GenerateSecret()
    if nil != secretErr {
        t.Fatalf("generating the fixture secret failed: %v", secretErr)
    }

    code, codeErr := totp.GenerateCodeAt(secret, time.Now(), totp.Config{})
    if nil != codeErr {
        t.Fatalf("generating the fixture code failed: %v", codeErr)
    }

    sharedGuard := melodysecurity.NewMemoryNonceGuard()
    headers := map[string]string{melodysecurity.DefaultTotpCodeHeaderName: code}

    store, connector := enrolledStore(t, "alice", secret)
    request, runtimeInstance := authenticatedTwoFactorRequest(t, "/twofactor/enroll", "alice", headers)
    response, handlerErr := EnrollHandler(fixedStore(store), sharedGuard)(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr || nil == response || nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected a current code to replace the enrollment with 200, got %v, %v", response, handlerErr)
    }

    if 1 != len(insertStatements(connector.recorded())) {
        t.Fatalf("expected the enrollment rewritten once, got %q", connector.recorded())
    }

    request, runtimeInstance = authenticatedTwoFactorRequest(t, "/twofactor/verify", "alice", headers)
    response, handlerErr = VerifyHandler(fixedStore(store), sharedGuard)(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr || nil == response || nethttp.StatusUnauthorized != response.StatusCode() {
        t.Fatalf("expected the code the replacement spent refused at the verification door, got %v, %v", response, handlerErr)
    }
}

/* a recovery code of the enrollment being replaced is redeemed before the replacement is written */
func TestEnrollHandlerReplacesWithARecoveryCodeRedeemedFirst(t *testing.T) {
    store, connector := enrolledStore(t, "alice", "JBSWY3DPEHPK3PXP")
    request, runtimeInstance := authenticatedTwoFactorRequest(t, "/twofactor/enroll", "alice", map[string]string{melodysecurity.DefaultTotpRecoveryHeaderName: "recovery-one"})

    response, handlerErr := EnrollHandler(fixedStore(store), nil)(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr || nil == response || nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected a recovery code to replace the enrollment with 200, got %v, %v, statements %q", response, handlerErr, connector.recorded())
    }

    statements := connector.recorded()
    redeemedAt, insertedAt := -1, -1
    for position, statement := range statements {
        if -1 == redeemedAt && true == strings.HasPrefix(statement, "UPDATE ") {
            redeemedAt = position
        }

        if true == strings.HasPrefix(statement, "INSERT INTO ") {
            insertedAt = position
        }
    }

    if -1 == redeemedAt || redeemedAt > insertedAt {
        t.Fatalf("expected the recovery code redeemed before the enrollment was rewritten, got %q", statements)
    }
}

/* recordingNonceGuard remembers nothing and records the ttl each accepted code is burned under */
type recordingNonceGuard struct {
    ttlList []time.Duration
}

func (instance *recordingNonceGuard) Remember(runtimeInstance melodyruntimecontract.Runtime, nonce string, ttl time.Duration) (bool, error) {
    instance.ttlList = append(instance.ttlList, ttl)

    return false, nil
}

/* a code stays acceptable for the whole skew window, a period on either side of its own, so it is burned for the whole of it: a burn of one period would let the code be replayed in the next */
func TestVerifyHandler_BurnsTheCodeForTheWholeValidityWindow(t *testing.T) {
    secret, secretErr := totp.GenerateSecret()
    if nil != secretErr {
        t.Fatalf("generating the fixture secret failed: %v", secretErr)
    }

    code, codeErr := totp.GenerateCodeAt(secret, time.Now(), totp.Config{})
    if nil != codeErr {
        t.Fatalf("generating the fixture code failed: %v", codeErr)
    }

    store, _ := enrolledStore(t, "alice", secret)
    request, runtimeInstance := authenticatedTwoFactorRequest(
        t,
        "/twofactor/verify",
        "alice",
        map[string]string{melodysecurity.DefaultTotpCodeHeaderName: code},
    )

    guard := &recordingNonceGuard{}

    response, handlerErr := VerifyHandler(fixedStore(store), guard)(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr || nil == response || nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected the code accepted, got %v, %v", response, handlerErr)
    }

    if 1 != len(guard.ttlList) || totpCodeValidityWindow() != guard.ttlList[0] || 90*time.Second != guard.ttlList[0] {
        t.Fatalf("expected the code burned once for the 90 s validity window, got %v", guard.ttlList)
    }
}
