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

    response, handlerErr := EnrollHandler(nil)(runtimeInstance, httptest.NewRecorder(), request)
    assertTwoFactorUnauthorized(t, "the enrollment door", response, handlerErr)
}

func TestVerifyHandlerRefusesACallerWithoutAToken(t *testing.T) {
    request, runtimeInstance := twoFactorRequest(t, "/twofactor/verify?user=admin")

    response, handlerErr := VerifyHandler(nil)(runtimeInstance, httptest.NewRecorder(), request)
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

    response, handlerErr := EnrollHandler(fixedStore(store))(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr || nil == response || nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected the enrollment to answer 200, got %v, %v", response, handlerErr)
    }

    statements := connector.recorded()
    if 1 != len(statements) || false == strings.HasPrefix(statements[0], "INSERT INTO ") {
        t.Fatalf("expected the enrollment to write one row and nothing else, got %q", statements)
    }

    if false == strings.Contains(statements[0], "'alice'") || true == strings.Contains(statements[0], "'admin'") {
        t.Fatalf("expected the enrollment to write the token's row and never the one the request named, got %q", statements[0])
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

    response, handlerErr := VerifyHandler(fixedStore(store))(runtimeInstance, httptest.NewRecorder(), request)
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

    response, handlerErr := VerifyHandler(fixedStore(store))(runtimeInstance, httptest.NewRecorder(), request)
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
    handler := EnrollHandler(storeSource)

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

    if 2 != *calls || 1 != len(connector.recorded()) {
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
    handler := VerifyHandler(storeSource)

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
