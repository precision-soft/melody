package config

import (
    "context"
    "fmt"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/.example/event"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyevent "github.com/precision-soft/melody/v4/event"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodykernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodysecurity "github.com/precision-soft/melody/v4/security"
)

func TestRequestBudgetConfig_ChargesTheForwardedClientRatherThanTheProxy(t *testing.T) {
    if key := resolvedBudgetKey(t, []string{balancerAddress}, balancerAddress, "203.0.113.7"); "203.0.113.7" != key {
        t.Fatalf("expected the forwarded client to be charged, got %q", key)
    }
}

func TestRequestBudgetConfig_IgnoresAForwardedHeaderFromAnUntrustedPeer(t *testing.T) {
    if key := resolvedBudgetKey(t, []string{balancerAddress}, "198.51.100.4", "203.0.113.7"); "198.51.100.4" != key {
        t.Fatalf("expected a header from an untrusted peer to be ignored, got %q", key)
    }
}

/* every host client enters through the docker bridge gateway, which nginx appends to the chain */
func TestRequestBudgetConfig_ReadsTheDockerGatewayAsTheClientRatherThanAsAHop(t *testing.T) {
    if key := resolvedBudgetKey(t, []string{balancerAddress}, balancerAddress, "172.18.0.1"); "172.18.0.1" != key {
        t.Fatalf("expected the gateway the balancer forwarded to be the client, got %q", key)
    }

    if key := resolvedBudgetKey(t, []string{balancerAddress}, balancerAddress, "203.0.113.7, 172.18.0.1"); "172.18.0.1" != key {
        t.Fatalf("expected a header the client sent through the balancer to stop at the gateway, got %q", key)
    }
}

func TestRequestBudgetConfig_DoesNotBelieveANeighbouringContainer(t *testing.T) {
    if key := resolvedBudgetKey(t, []string{balancerAddress}, "172.18.0.11", "203.0.113.7"); "172.18.0.11" != key {
        t.Fatalf("expected a header from a neighbouring container to be ignored, got %q", key)
    }
}

func TestRequestBudgetConfig_AnEmptyListChargesThePeer(t *testing.T) {
    if key := resolvedBudgetKey(t, nil, balancerAddress, "203.0.113.7"); balancerAddress != key {
        t.Fatalf("expected an empty trusted list to charge the peer, got %q", key)
    }
}

func TestRegisterSubscribers_InstallsTheTwoFactorEnrollmentReleaseWhenThereIsADatabase(t *testing.T) {
    ownersOnDeletion := func(moduleInstance *Module) []string {
        eventDispatcher := melodyevent.NewEventDispatcher(melodyevent.EventDispatcherConfig{Clock: melodyclock.NewSystemClock()})
        moduleInstance.registerSubscribers(eventDispatcher)

        var owners []string
        for _, registered := range eventDispatcher.RegisteredEvents() {
            if event.EventUserDeleted != registered.EventName {
                continue
            }

            for _, listener := range registered.Listeners {
                owners = append(owners, listener.Owner)
            }
        }

        return owners
    }

    without := ownersOnDeletion(moduleWithEnvironment(t, map[string]string{}))

    withStore := moduleWithEnvironment(t, map[string]string{})
    withStore.catalogueWired = true
    with := ownersOnDeletion(withStore)

    if len(without)+1 != len(with) {
        t.Fatalf("expected the database to add one owner on %s, got %v without and %v with", event.EventUserDeleted, without, with)
    }

    found := false
    for _, owner := range with {
        if true == strings.Contains(owner, "TwoFactorEnrollmentSubscriber") {
            found = true
        }
    }

    if false == found {
        t.Fatalf("expected the enrollment subscriber among the owners of %s, got %v", event.EventUserDeleted, with)
    }
}

func TestRegisterSubscribers_TheEnrollmentReleaseOutranksTheCacheClearOnUserDeleted(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.catalogueWired = true

    eventDispatcher := melodyevent.NewEventDispatcher(melodyevent.EventDispatcherConfig{Clock: melodyclock.NewSystemClock()})
    moduleInstance.registerSubscribers(eventDispatcher)

    releasePriority, cachePriority := 0, 0
    releaseFound, cacheFound := false, false
    for _, registered := range eventDispatcher.RegisteredEvents() {
        if event.EventUserDeleted != registered.EventName {
            continue
        }

        for _, listener := range registered.Listeners {
            if true == strings.Contains(listener.Owner, "TwoFactorEnrollmentSubscriber") {
                releasePriority, releaseFound = listener.Config.Priority, true
            }
            if true == strings.Contains(listener.Owner, "UserEventSubscriber") {
                cachePriority, cacheFound = listener.Config.Priority, true
            }
        }
    }

    if false == releaseFound || false == cacheFound {
        t.Fatalf("expected both the release and the cache subscriber on %s, found release %v and cache %v", event.EventUserDeleted, releaseFound, cacheFound)
    }
    if releasePriority <= cachePriority {
        t.Fatalf("expected the release (%d) to outrank the cache clear (%d) on %s", releasePriority, cachePriority, event.EventUserDeleted)
    }
}

func TestRegisterSubscribers_DeclaresTheDeletionsCleanupTags(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.catalogueWired = true

    eventDispatcher := melodyevent.NewEventDispatcher(melodyevent.EventDispatcherConfig{Clock: melodyclock.NewSystemClock()})
    moduleInstance.registerSubscribers(eventDispatcher)

    for _, registered := range eventDispatcher.RegisteredEvents() {
        if event.EventUserDeleted != registered.EventName {
            continue
        }

        if false == registered.Declared || "release,cache" != strings.Join(registered.Config.TagsToRunAfterFailure, ",") {
            t.Fatalf("expected %s declared with the release and cache tags, got %+v", event.EventUserDeleted, registered)
        }

        if 3 != len(registered.Listeners) {
            t.Fatalf("expected the three listeners of the deletion, got %+v", registered.Listeners)
        }

        for _, listener := range registered.Listeners {
            if false == listener.RunsAfterFailure {
                t.Fatalf("expected every listener of the deletion to run after a failure, got %+v", listener)
            }
        }

        return
    }

    t.Fatalf("expected %s among the registered events", event.EventUserDeleted)
}

func TestRequestBudgetLimiter_AdmitsTheBudgetPerHourAndRefusesTheNext(t *testing.T) {
    if time.Hour != requestBudgetWindow {
        t.Fatalf("expected the budget counted per hour, got %s", requestBudgetWindow)
    }

    limiter := requestBudgetLimiter(3)
    for request := 1; request <= 3; request++ {
        if false == allowedBy(limiter, "203.0.113.7") {
            t.Fatalf("expected request %d of a budget of 3 admitted", request)
        }
    }

    if true == allowedBy(limiter, "203.0.113.7") {
        t.Fatal("expected the fourth request of a budget of 3 refused")
    }
}

func TestRegisterRateLimitRequestListener_TheArmedDoorRefusesTheRequestPastTheSwitchBudget(t *testing.T) {
    eventDispatcher := melodyevent.NewEventDispatcher(melodyevent.EventDispatcherConfig{Clock: melodyclock.NewSystemClock()})
    moduleInstance := moduleWithEnvironment(t, map[string]string{environmentKeyRequestBudgetPerHour: "2"})
    moduleInstance.trustedProxyResolver = newTrustedProxyResolver("", time.Now)
    moduleInstance.registerRateLimitRequestListener(eventDispatcher)

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    scope := containerInstance.NewScope()
    scope.MustOverrideProtectedInstance(melodylogging.ServiceLogger, melodylogging.NewNopLogger(), melodycontainer.OverrideOptions{})
    runtimeInstance := melodyruntime.NewRun(context.Background(), scope)

    for request := 1; request <= 3; request++ {
        httpRequest := httptest.NewRequest(nethttp.MethodGet, "/", nil)
        httpRequest.RemoteAddr = "203.0.113.7:41234"
        requestEvent := melodyhttp.NewKernelRequestEvent(runtimeInstance, melodyhttp.NewRequest(httpRequest, runtimeInstance, melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("budget-test", time.Now())}))

        if _, dispatchErr := eventDispatcher.DispatchName(runtimeInstance, melodykernelcontract.EventKernelRequest, requestEvent); nil != dispatchErr {
            t.Fatalf("unexpected dispatch error on request %d: %v", request, dispatchErr)
        }

        refusal := melodyexception.AsHttpException(requestEvent.Refusal())
        refused := nil != refusal && nethttp.StatusTooManyRequests == refusal.StatusCode()
        if (3 == request) != refused {
            t.Fatalf("expected only the third request of a budget of 2 refused, request %d refused: %t", request, refused)
        }
    }
}

func TestRegisterRateLimitRequestListener_ArmsOnAPositiveBudgetAndRefusesAnyOtherValueByName(t *testing.T) {
    kernelRequestListeners := func(moduleInstance *Module) int {
        eventDispatcher := melodyevent.NewEventDispatcher(melodyevent.EventDispatcherConfig{Clock: melodyclock.NewSystemClock()})
        moduleInstance.registerRateLimitRequestListener(eventDispatcher)

        count := 0
        for _, registered := range eventDispatcher.RegisteredEvents() {
            count = count + len(registered.Listeners)
        }

        return count
    }

    if 0 != kernelRequestListeners(moduleWithEnvironment(t, map[string]string{})) {
        t.Fatal("expected the door unwired without the switch")
    }

    if 1 != kernelRequestListeners(moduleWithEnvironment(t, map[string]string{environmentKeyRequestBudgetPerHour: " 120 "})) {
        t.Fatal("expected the door armed on a positive budget")
    }

    for _, value := range []string{"abc", "0", "-5", "1.5"} {
        refusal := func() (recovered any) {
            defer func() {
                recovered = recover()
            }()

            kernelRequestListeners(moduleWithEnvironment(t, map[string]string{environmentKeyRequestBudgetPerHour: value}))

            return nil
        }()

        if nil == refusal || false == strings.Contains(fmt.Sprint(refusal), "invalid environment value") {
            t.Fatalf("expected %q refused by name, got %v", value, refusal)
        }
    }
}

func TestRequestBudgetLimiter_BoundsTheAddressesItTracksByEvictingTheOldest(t *testing.T) {
    limiter := requestBudgetLimiter(1)

    for address := 0; address < inProcessWriteThrottleMaxAddresses; address++ {
        if false == allowedBy(limiter, fmt.Sprintf("address-%d", address)) {
            t.Fatalf("expected address %d admitted below the ceiling", address)
        }
    }

    if true == allowedBy(limiter, "address-1") {
        t.Fatal("expected a tracked address to keep its spent budget")
    }

    if false == allowedBy(limiter, "address-past-the-ceiling") {
        t.Fatalf("expected an unseen address admitted once %d addresses are tracked", inProcessWriteThrottleMaxAddresses)
    }

    if false == allowedBy(limiter, "address-0") {
        t.Fatal("expected the address used longest ago evicted and back with a full budget")
    }
}

func TestRegisterApiExceptionListener_AnswersTheApiPathsAndLeavesTheOthersToTheBuiltIn(t *testing.T) {
    eventDispatcher := melodyevent.NewEventDispatcher(melodyevent.EventDispatcherConfig{Clock: melodyclock.NewSystemClock()})
    moduleWithEnvironment(t, map[string]string{}).registerApiExceptionListener(eventDispatcher)

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    scope := containerInstance.NewScope()
    scope.MustOverrideProtectedInstance(melodylogging.ServiceLogger, melodylogging.NewNopLogger(), melodycontainer.OverrideOptions{})
    runtimeInstance := melodyruntime.NewRun(context.Background(), scope)

    dispatchRefusal := func(path string, refusal error) *melodyhttp.KernelExceptionEvent {
        request := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodGet, path, nil), runtimeInstance, melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("api-test", time.Now())})
        exceptionEvent := melodyhttp.NewKernelExceptionEvent(runtimeInstance, request, refusal, melodyhttp.KernelExceptionEventConfig{})

        if _, dispatchErr := eventDispatcher.DispatchName(runtimeInstance, melodykernelcontract.EventKernelException, exceptionEvent); nil != dispatchErr {
            t.Fatalf("unexpected dispatch error: %v", dispatchErr)
        }

        return exceptionEvent
    }

    answered := dispatchRefusal("/products/api/read/", melodyexception.Unauthorized("unauthorized"))
    if nil == answered.Response() || nethttp.StatusUnauthorized != answered.Response().StatusCode() {
        t.Fatalf("expected the envelope's 401 under the api paths, got %#v", answered.Response())
    }

    body, readErr := io.ReadAll(answered.Response().BodyReader())
    if nil != readErr {
        t.Fatalf("unexpected read error: %v", readErr)
    }

    if false == strings.Contains(string(body), `"success":false`) || false == strings.Contains(string(body), "unauthorized") {
        t.Fatalf("expected the example's envelope, got %s", body)
    }

    plain := dispatchRefusal("/products/api/read/", fmt.Errorf("a store failed"))
    if nil == plain.Response() || nethttp.StatusInternalServerError != plain.Response().StatusCode() {
        t.Fatalf("expected a plain error to be a 500 under the api paths, got %#v", plain.Response())
    }

    if declined := dispatchRefusal("/products", melodyexception.NotFound("no such page")); nil != declined.Response() {
        t.Fatalf("expected an html route left to the built-in, got %#v", declined.Response())
    }

    challenge := dispatchRefusal("/login/check", melodyexception.NewHttpError(nethttp.StatusUnauthorized, melodyexception.HttpErrorConfig{Message: "second factor required", Cause: melodysecurity.ErrSecondFactorRequired}))
    if nil == challenge.Response() || nethttp.StatusUnauthorized != challenge.Response().StatusCode() {
        t.Fatalf("expected the sign-in door answered in the envelope, got %#v", challenge.Response())
    }

    challengeBody, challengeReadErr := io.ReadAll(challenge.Response().BodyReader())
    if nil != challengeReadErr || false == strings.Contains(string(challengeBody), `"factor":"totp"`) {
        t.Fatalf("expected the challenge to name the factor, got %s and %v", challengeBody, challengeReadErr)
    }

    if declined := dispatchRefusal("/login%2Fcheck", melodyexception.NotFound("no such page")); nil != declined.Response() {
        t.Fatalf("expected an encoded separator not to alias the door, got %#v", declined.Response())
    }
}

func TestRegisterSubscribers_InstallsTheApiExceptionListener(t *testing.T) {
    eventDispatcher := melodyevent.NewEventDispatcher(melodyevent.EventDispatcherConfig{Clock: melodyclock.NewSystemClock()})
    moduleWithEnvironment(t, map[string]string{}).registerSubscribers(eventDispatcher)

    for _, registered := range eventDispatcher.RegisteredEvents() {
        if melodykernelcontract.EventKernelException == registered.EventName && 1 == len(registered.Listeners) {
            return
        }
    }

    t.Fatalf("expected the composition root to install the api exception listener")
}
