package twofactor

import (
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* twoFactorRequest builds a request whose runtime carries NO security context, which is what an
   unauthenticated caller reaches these doors with. The store is nil on purpose: the refusal has to be
   decided before either door touches it, so a nil handle that would panic if it were reached is the
   assertion that the guard stands first. */
func twoFactorRequest(t *testing.T, target string) (*melodyhttp.Request, melodyruntimecontract.Runtime) {
    t.Helper()

    containerInstance := melodycontainer.NewContainer()
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    request := melodyhttp.NewRequest(
        httptest.NewRequest(nethttp.MethodPost, target, nil),
        nil,
        runtimeInstance,
        melodyhttp.NewRequestContext("two-factor-test", time.Now()),
    )

    return request, runtimeInstance
}

/* both doors used to take the account from a `user` query parameter, on a route that carried
   PUBLIC_ACCESS. Measured on the running stack, that pair answered 200 to an anonymous
   `POST /twofactor/enroll?user=admin` and wrote the administrator's row, handing the caller the secret
   that satisfies the factor — and the administrator could never enroll after that, the insert being a
   plain one. The identifier is the token's now, so a caller without one has nothing to name and is
   refused before anything is written.

   The query parameter is still sent here, because the assertion that matters is that it is NOT read: a
   handler that fell back to it would answer something other than 401 for this request. */
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
