package twofactor

import (
    "context"
    "encoding/json"
    "errors"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "regexp"
    "strings"
    "testing"
    "time"

    examplesecurity "github.com/precision-soft/melody/v4/.example/security"
    store2fa "github.com/precision-soft/melody/v4/.example/twofactor"
    "github.com/precision-soft/melody/v4/bound"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodysecurity "github.com/precision-soft/melody/v4/security"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/precision-soft/melody/v4/security/totp"
)

/* the account is the token's, so a caller without a token has nothing to name and is refused before anything is written. The `user` query parameter is sent anyway, because the assertion that matters is that it is not read: a handler that fell back to it would answer something other than 401. The store is nil on purpose: a nil handle that would panic if reached asserts that the guard stands first. */
func TestEnrollHandlerRefusesACallerWithoutAToken(t *testing.T) {
    request, runtimeInstance := twoFactorRequest(t, "/twofactor/enroll?user=admin")

    response, handlerErr := EnrollHandler(nil, nil, nil)(runtimeInstance, httptest.NewRecorder(), request)
    assertTwoFactorUnauthorized(t, "the enrollment door", response, handlerErr)
}

func TestVerifyHandlerRefusesACallerWithoutAToken(t *testing.T) {
    request, runtimeInstance := twoFactorRequest(t, "/twofactor/verify?user=admin")

    response, handlerErr := VerifyHandler(nil, nil, nil)(runtimeInstance, httptest.NewRecorder(), request)
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

    response, handlerErr := EnrollHandler(fixedStore(store), nil, nil)(runtimeInstance, httptest.NewRecorder(), request)
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

    response, handlerErr := VerifyHandler(fixedStore(store), nil, nil)(runtimeInstance, httptest.NewRecorder(), request)
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

    response, handlerErr := VerifyHandler(fixedStore(store), nil, nil)(runtimeInstance, httptest.NewRecorder(), request)
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

    return func(runtimeInstance melodycontainercontract.Runtime) (*store2fa.Store, error) {
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
    handler := EnrollHandler(storeSource, nil, nil)

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
    handler := VerifyHandler(storeSource, nil, nil)

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

    sharedGuard := melodysecurity.NewMemoryNonceGuard(melodysecurity.MemoryNonceGuardConfig{})
    if seen, rememberErr := sharedGuard.Remember(runtimeInstance, "2fa:alice:"+totp.NormalizeCode(code), bound.Of(time.Minute)); nil != rememberErr || true == seen {
        t.Fatalf("burning the code as the sign-in does failed: %v %v", seen, rememberErr)
    }

    response, handlerErr := VerifyHandler(fixedStore(store), sharedGuard, nil)(runtimeInstance, httptest.NewRecorder(), request)
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

        response, handlerErr := EnrollHandler(fixedStore(store), nil, nil)(runtimeInstance, httptest.NewRecorder(), request)
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

    sharedGuard := melodysecurity.NewMemoryNonceGuard(melodysecurity.MemoryNonceGuardConfig{})
    headers := map[string]string{melodysecurity.DefaultTotpCodeHeaderName: code}

    store, connector := enrolledStore(t, "alice", secret)
    request, runtimeInstance := authenticatedTwoFactorRequest(t, "/twofactor/enroll", "alice", headers)
    response, handlerErr := EnrollHandler(fixedStore(store), sharedGuard, nil)(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr || nil == response || nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected a current code to replace the enrollment with 200, got %v, %v", response, handlerErr)
    }

    if 1 != len(insertStatements(connector.recorded())) {
        t.Fatalf("expected the enrollment rewritten once, got %q", connector.recorded())
    }

    request, runtimeInstance = authenticatedTwoFactorRequest(t, "/twofactor/verify", "alice", headers)
    response, handlerErr = VerifyHandler(fixedStore(store), sharedGuard, nil)(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr || nil == response || nethttp.StatusUnauthorized != response.StatusCode() {
        t.Fatalf("expected the code the replacement spent refused at the verification door, got %v, %v", response, handlerErr)
    }
}

/* a recovery code of the enrollment being replaced is redeemed before the replacement is written */
func TestEnrollHandlerReplacesWithARecoveryCodeRedeemedFirst(t *testing.T) {
    store, connector := enrolledStore(t, "alice", "JBSWY3DPEHPK3PXP")
    request, runtimeInstance := authenticatedTwoFactorRequest(t, "/twofactor/enroll", "alice", map[string]string{melodysecurity.DefaultTotpRecoveryHeaderName: "recovery-one"})

    response, handlerErr := EnrollHandler(fixedStore(store), nil, nil)(runtimeInstance, httptest.NewRecorder(), request)
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
    ttlList []bound.Duration
}

func (instance *recordingNonceGuard) Remember(runtimeInstance melodycontainercontract.Runtime, nonce string, ttl bound.Duration) (bool, error) {
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

    response, handlerErr := VerifyHandler(fixedStore(store), guard, nil)(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr || nil == response || nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected the code accepted, got %v, %v", response, handlerErr)
    }

    window, windowErr := totpCodeValidityWindow()
    if nil != windowErr || 1 != len(guard.ttlList) || bound.Of(window) != guard.ttlList[0] || bound.Of(90*time.Second) != guard.ttlList[0] {
        t.Fatalf("expected the code burned once for the 90 s validity window, got %v", guard.ttlList)
    }
}

type countingFactorBudget struct {
    allowance int
    spent     int
    releases  int
    spendErr  error
}

func (instance *countingFactorBudget) Spend(melodycontainercontract.Runtime, string) error {
    instance.spent++
    if nil != instance.spendErr {
        return instance.spendErr
    }

    if instance.allowance < instance.spent {
        return &examplesecurity.SecondFactorBudgetSpentError{RetryAfter: 30 * time.Second}
    }

    return nil
}

func (instance *countingFactorBudget) Release(melodycontainercontract.Runtime, string) error {
    instance.releases++
    instance.spent = 0

    return nil
}

func TestTheSecondFactorDoorsSpendTheAccountsBudgetOnEveryPresentedFactor(t *testing.T) {
    secret, secretErr := totp.GenerateSecret()
    if nil != secretErr {
        t.Fatalf("generating the fixture secret failed: %v", secretErr)
    }

    for name, door := range map[string]func(store2fa.StoreSource, melodysecuritycontract.NonceGuard, FactorBudget) melodyhttpcontract.Handler{
        "verify": VerifyHandler,
        "enroll": EnrollHandler,
    } {
        store, _ := enrolledStore(t, "alice", secret)
        budget := &countingFactorBudget{allowance: 2}
        handler := door(fixedStore(store), nil, budget)

        statuses := make([]int, 0, 3)
        for range 3 {
            request, runtimeInstance := authenticatedTwoFactorRequest(t, "/twofactor/"+name, "alice", map[string]string{melodysecurity.DefaultTotpCodeHeaderName: "000000"})

            response, handlerErr := handler(runtimeInstance, httptest.NewRecorder(), request)
            if nil != handlerErr || nil == response {
                t.Fatalf("%s: unexpected answer %v, %v", name, response, handlerErr)
            }

            statuses = append(statuses, response.StatusCode())
        }

        if nethttp.StatusUnauthorized != statuses[0] || nethttp.StatusUnauthorized != statuses[1] || nethttp.StatusTooManyRequests != statuses[2] {
            t.Fatalf("%s: expected two refused guesses then 429, got %v", name, statuses)
        }

        if 0 != budget.releases {
            t.Fatalf("%s: expected a refused factor to give nothing back, got %d releases", name, budget.releases)
        }
    }
}

func pendingFixture(t *testing.T) (*store2fa.Store, *enrollmentConnector, string) {
    t.Helper()

    secret, secretErr := totp.GenerateSecret()
    if nil != secretErr {
        t.Fatalf("generating the fixture secret failed: %v", secretErr)
    }

    store, connector := enrolledStore(t, "", "")
    withPending(t, connector, "alice", secret)

    return store, connector, secret
}

func confirmWith(t *testing.T, handler melodyhttpcontract.Handler, headers map[string]string) melodyhttpcontract.Response {
    t.Helper()

    request, runtimeInstance := authenticatedTwoFactorRequest(t, "/twofactor/enroll/confirm", "alice", headers)
    response, handlerErr := handler(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr || nil == response {
        t.Fatalf("confirm: %v, %v", response, handlerErr)
    }

    return response
}

/* the enrollment door writes the pending table alone: a secret nobody proved the authenticator holds is no factor */
func TestEnrollHandlerWritesThePendingEnrollmentOnly(t *testing.T) {
    store, connector := enrolledStore(t, "", "")

    request, runtimeInstance := authenticatedTwoFactorRequest(t, "/twofactor/enroll", "alice", nil)
    response, handlerErr := EnrollHandler(fixedStore(store), nil, nil)(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr || nil == response || nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("enroll: %v, %v", response, handlerErr)
    }

    inserts := insertStatements(connector.recorded())
    if 1 != len(inserts) || false == strings.HasPrefix(inserts[0], "INSERT INTO `melody_example_v4_two_factor_pending`") {
        t.Fatalf("expected one write, to the pending table, got %q", inserts)
    }
}

/* a code of the pending secret moves the pending enrollment into the active table and empties the pending one */
func TestConfirmHandlerMovesThePendingEnrollmentOnItsCode(t *testing.T) {
    store, connector, secret := pendingFixture(t)

    code, codeErr := totp.GenerateCodeAt(secret, time.Now(), totp.Config{})
    if nil != codeErr {
        t.Fatalf("code: %v", codeErr)
    }

    response := confirmWith(t, ConfirmHandler(fixedStore(store), nil, nil), map[string]string{melodysecurity.DefaultTotpCodeHeaderName: code})
    if nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected 200, got %d", response.StatusCode())
    }

    lockIndex, insertIndex, deleteIndex := -1, -1, -1
    for index, statement := range connector.recorded() {
        switch {
        case strings.Contains(statement, "melody_example_v4_two_factor_pending") && strings.Contains(statement, "FOR UPDATE") && -1 == lockIndex:
            lockIndex = index
        case strings.HasPrefix(statement, "INSERT INTO `melody_example_v4_two_factor` ") && strings.Contains(statement, "pending-one") == false:
            insertIndex = index
        case strings.HasPrefix(statement, "DELETE FROM `melody_example_v4_two_factor_pending`"):
            deleteIndex = index
        }
    }

    if -1 == lockIndex || lockIndex > insertIndex || insertIndex > deleteIndex {
        t.Fatalf("expected the pending row locked, then moved, then deleted, got %q", connector.recorded())
    }
}

/* a wrong code leaves the pending enrollment where it is and writes nothing */
func TestConfirmHandlerRefusesAWrongCodeWritingNothing(t *testing.T) {
    store, connector, _ := pendingFixture(t)

    response := confirmWith(t, ConfirmHandler(fixedStore(store), nil, nil), map[string]string{melodysecurity.DefaultTotpCodeHeaderName: "000000"})
    if nethttp.StatusUnauthorized != response.StatusCode() {
        t.Fatalf("expected 401, got %d", response.StatusCode())
    }

    for _, statement := range connector.recorded() {
        if true == strings.HasPrefix(statement, "INSERT") || true == strings.HasPrefix(statement, "DELETE") {
            t.Fatalf("expected nothing written, got %q", statement)
        }
    }
}

/* a code is spent once: the same code presented twice is refused the second time, as at the sign-in */
func TestConfirmHandlerRefusesAReplayedCode(t *testing.T) {
    store, _, secret := pendingFixture(t)
    code, _ := totp.GenerateCodeAt(secret, time.Now(), totp.Config{})
    guard := melodysecurity.NewMemoryNonceGuard(melodysecurity.MemoryNonceGuardConfig{})
    handler := ConfirmHandler(fixedStore(store), guard, nil)

    if first := confirmWith(t, handler, map[string]string{melodysecurity.DefaultTotpCodeHeaderName: code}); nethttp.StatusOK != first.StatusCode() {
        t.Fatalf("expected the first confirmation accepted, got %d", first.StatusCode())
    }

    if second := confirmWith(t, handler, map[string]string{melodysecurity.DefaultTotpCodeHeaderName: code}); nethttp.StatusUnauthorized != second.StatusCode() {
        t.Fatalf("expected the replay refused, got %d", second.StatusCode())
    }
}

func TestConfirmHandlerAnswersConflictWhenNothingIsPending(t *testing.T) {
    store, _ := enrolledStore(t, "", "")

    if response := confirmWith(t, ConfirmHandler(fixedStore(store), nil, nil), map[string]string{melodysecurity.DefaultTotpCodeHeaderName: "123456"}); nethttp.StatusConflict != response.StatusCode() {
        t.Fatalf("expected 409, got %d", response.StatusCode())
    }
}

func TestConfirmHandlerAsksForACode(t *testing.T) {
    store, _, _ := pendingFixture(t)

    if response := confirmWith(t, ConfirmHandler(fixedStore(store), nil, nil), nil); nethttp.StatusBadRequest != response.StatusCode() {
        t.Fatalf("expected 400, got %d", response.StatusCode())
    }
}

/* the confirmation spends the account's budget like the sign-in, so a session cannot walk the codes of a secret it holds; an account past it is refused before its code is read */
func TestConfirmHandlerSpendsTheFactorBudget(t *testing.T) {
    store, _, _ := pendingFixture(t)
    budget := &countingFactorBudget{allowance: 0}

    response := confirmWith(t, ConfirmHandler(fixedStore(store), nil, budget), map[string]string{melodysecurity.DefaultTotpCodeHeaderName: "123456"})
    if nethttp.StatusTooManyRequests != response.StatusCode() || 1 != budget.spent {
        t.Fatalf("expected 429 after one spend, got %d and %d spends", response.StatusCode(), budget.spent)
    }

    if "30" != response.Headers().Get("Retry-After") {
        t.Fatalf("expected the budget's Retry-After on the 429, got %q", response.Headers().Get("Retry-After"))
    }
}

func TestConfirmHandlerAnswersABudgetFailureByWhetherItIsAnOutage(t *testing.T) {
    for name, testCase := range map[string]struct {
        spendErr error
        status   int
    }{
        "outage":        {spendErr: melodyexception.MarkInfrastructureFailure(errors.New("connection refused")), status: nethttp.StatusServiceUnavailable},
        "other failure": {spendErr: errors.New("runtime is nil"), status: nethttp.StatusInternalServerError},
    } {
        store, _, _ := pendingFixture(t)
        budget := &countingFactorBudget{spendErr: testCase.spendErr}

        if response := confirmWith(t, ConfirmHandler(fixedStore(store), nil, budget), map[string]string{melodysecurity.DefaultTotpCodeHeaderName: "123456"}); testCase.status != response.StatusCode() {
            t.Fatalf("%s: expected %d, got %d", name, testCase.status, response.StatusCode())
        }
    }
}

func TestStatusHandlerAnswersBothStates(t *testing.T) {
    store, _, _ := pendingFixture(t)

    request, runtimeInstance := authenticatedTwoFactorRequest(t, "/twofactor/status", "alice", nil)
    response, handlerErr := StatusHandler(fixedStore(store))(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr || nil == response || nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("status: %v, %v", response, handlerErr)
    }

    bodyBytes, _ := io.ReadAll(response.BodyReader())
    body := string(bodyBytes)
    if false == strings.Contains(body, `"enrolled":false`) || false == strings.Contains(body, `"pending":true`) {
        t.Fatalf("expected not enrolled and pending, got %s", body)
    }
}


/* a second enroll between the confirmation's read and its move replaces the pending secret: the move holds the row and refuses a secret other than the one the code was verified against, writing nothing */
func TestConfirmEnrollmentRefusesAPendingSecretReplacedSinceTheCodeWasVerified(t *testing.T) {
    store, connector, _ := pendingFixture(t)

    outcome, confirmErr := store.ConfirmEnrollment(context.Background(), "alice", "ANOTHERSECRETTHANTHEPENDINGONE22")
    if nil != confirmErr || store2fa.ConfirmReplaced != outcome {
        t.Fatalf("expected the move refused as replaced, got %v, %v", outcome, confirmErr)
    }

    for _, statement := range connector.recorded() {
        if true == strings.HasPrefix(statement, "INSERT") || true == strings.HasPrefix(statement, "DELETE") {
            t.Fatalf("expected nothing written, got %q", statement)
        }
    }
}

/* a set-up started on the session alone replaces no factor: one that became active since is kept, and nothing is written */
func TestConfirmHandlerKeepsAFactorThatBecameActiveSinceAnUngrantedSetUp(t *testing.T) {
    store, connector, secret := pendingFixture(t)
    connector.enrolledIdentifier = "alice"
    connector.sealedSecret = connector.pendingSealedSecret

    code, _ := totp.GenerateCodeAt(secret, time.Now(), totp.Config{})
    response := confirmWith(t, ConfirmHandler(fixedStore(store), nil, nil), map[string]string{melodysecurity.DefaultTotpCodeHeaderName: code})
    if nethttp.StatusConflict != response.StatusCode() {
        t.Fatalf("expected 409, got %d", response.StatusCode())
    }

    for _, statement := range connector.recorded() {
        if true == strings.HasPrefix(statement, "INSERT") || true == strings.HasPrefix(statement, "DELETE") {
            t.Fatalf("expected nothing written, got %q", statement)
        }
    }
}

/* a set-up that presented the active factor replaces it */
func TestConfirmHandlerReplacesTheFactorAGrantedSetUpPresented(t *testing.T) {
    store, connector, secret := pendingFixture(t)
    connector.enrolledIdentifier = "alice"
    connector.pendingReplaces = true

    code, _ := totp.GenerateCodeAt(secret, time.Now(), totp.Config{})
    if response := confirmWith(t, ConfirmHandler(fixedStore(store), nil, nil), map[string]string{melodysecurity.DefaultTotpCodeHeaderName: code}); nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected 200, got %d", response.StatusCode())
    }
}

/* the confirmation names its set-up: a later enroll in its place is answered 409, not 401 against a secret the sender never saw */
func TestConfirmHandlerAnswersConflictForASetUpReplacedSince(t *testing.T) {
    store, _, secret := pendingFixture(t)

    code, _ := totp.GenerateCodeAt(secret, time.Now(), totp.Config{})
    response := confirmWith(t, ConfirmHandler(fixedStore(store), nil, nil), map[string]string{melodysecurity.DefaultTotpCodeHeaderName: code, PendingHeaderName: "0000000000000000"})
    if nethttp.StatusConflict != response.StatusCode() {
        t.Fatalf("expected 409, got %d", response.StatusCode())
    }

    if response := confirmWith(t, ConfirmHandler(fixedStore(store), nil, nil), map[string]string{melodysecurity.DefaultTotpCodeHeaderName: code, PendingHeaderName: pendingIdentifierOf(secret)}); nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected the set-up it names confirmed, got %d", response.StatusCode())
    }
}

/* the enrollment records whether the caller presented the active factor, the grant the confirmation reads */
func TestEnrollHandlerRecordsTheReplacementGrant(t *testing.T) {
    store, connector := enrolledStore(t, "", "")

    request, runtimeInstance := authenticatedTwoFactorRequest(t, "/twofactor/enroll", "alice", nil)
    if _, handlerErr := EnrollHandler(fixedStore(store), nil, nil)(runtimeInstance, httptest.NewRecorder(), request); nil != handlerErr {
        t.Fatalf("enroll: %v", handlerErr)
    }

    inserts := insertStatements(connector.recorded())
    if 1 != len(inserts) || false == strings.Contains(inserts[0], "replaces_factor = VALUES(replaces_factor)") || false == strings.Contains(inserts[0], ", FALSE, ") {
        t.Fatalf("expected a pending row without the grant, got %q", inserts)
    }
}


/* a pending enrollment is confirmable for its lifetime alone: every read of the pending table, the confirmation's lock and the state included, asks for a row younger than PendingEnrollmentLifetime */
func TestPendingReadsAskForARowWithinItsLifetime(t *testing.T) {
    store, connector, secret := pendingFixture(t)

    _, _, _ = store.FindPendingSecret(context.Background(), "alice")
    _, _ = store.ConfirmEnrollment(context.Background(), "alice", secret)
    _, _, _ = store.EnrollmentState(context.Background(), "alice")

    cutoffPattern := regexp.MustCompile(`created_at > '([0-9-]+ [0-9:.]+)'`)
    expected := time.Now().UTC().Add(-store2fa.PendingEnrollmentLifetime)
    reads := 0
    for _, statement := range connector.recorded() {
        if false == strings.Contains(statement, "melody_example_v4_two_factor_pending") || true == strings.HasPrefix(statement, "INSERT") || true == strings.HasPrefix(statement, "DELETE") {
            continue
        }

        match := cutoffPattern.FindStringSubmatch(statement)
        if nil == match {
            t.Fatalf("expected the read bounded by the lifetime, got %q", statement)
        }

        cutoff, parseErr := time.Parse("2006-01-02 15:04:05.999999", match[1])
        if nil != parseErr || time.Minute < cutoff.Sub(expected).Abs() {
            t.Fatalf("expected the cutoff near %v, got %q (%v)", expected, match[1], parseErr)
        }

        reads++
    }

    if 3 != reads {
        t.Fatalf("expected the three pending reads bounded, got %d in %q", reads, connector.recorded())
    }
}
