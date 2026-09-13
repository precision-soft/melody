package main

import (
    "context"
    "fmt"
    "io"
    "net"
    "net/http"
    "net/http/cookiejar"
    "net/url"
    "strings"
    "time"
)

/* exampleRateLimitBudget mirrors the write allowance the example wires on the nomenclature's write endpoints (config/redis.go, catalogWriteAllowance). */
const exampleRateLimitBudget = 30

/* exampleRateLimitPrefix mirrors the key prefix the supervised v3 example gives its redis rate limiter, so the harness can clear the counters of a previous run rather than inherit a spent budget inside the same fixed window. It carries the major for the same reason the application does: three applications share one redis. */
const exampleRateLimitPrefix = "melody-example-v3:rate_limit:"

/* the throttled endpoint is a real one — creating a product — so the body below is deliberately invalid: the rate-limit middleware runs before the handler, so a refused write still spends the allowance and the catalogue is left exactly as it was. */
const (
    exampleThrottledWriteRoute = "/products/api/create/"
    exampleThrottledWriteBody  = `{"name":""}`

    exampleHttpEditorUsername = "editor"
    exampleHttpEditorPassword = "editor"

    exampleHttpAdminUsername = "admin"
    exampleHttpAdminPassword = "admin"

    /* the seeded account that holds ROLE_USER and nothing else: what a section needs when it has to drive an authenticated route as somebody who is NOT the one under test. */
    exampleHttpUserUsername = "user"
    exampleHttpUserPassword = "user"
)

/* runExampleHttpCheck drives the running .example application over real HTTP — the only place the whole
chain is exercised together: nginx sets X-Forwarded-For, the forwarded-client-ip resolver decides which hop
to trust, and the distributed rate limiter counts against the resolved key in redis.

Two properties matter and neither can be proven in-process:

  - through the load balancer the budget is enforced across requests and the request past it gets a 429;
  - straight to the application from an UNTRUSTED peer (loopback is outside the example's trusted proxy
    list) a spoofed X-Forwarded-For is ignored, so an attacker cannot mint a fresh budget per fake address.

The budget sits on what changes the nomenclature, so the section signs in first: the firewall answers an
anonymous write with 401 before the limiter ever sees it, and a section that never reached the limiter would
report a budget it had not measured.

The section runs when EXAMPLE_BASE_URL is set and needs REDIS_ADDRESS to clear the counters first. */
func runExampleHttpCheck(baseUrl string, loadBalancerUrl string, redisAddress string) {
    resetExampleRateLimitCounters("example http", redisAddress, exampleRateLimitPrefix)

    client := newExampleHttpClient()
    signInExampleHttpEditor(client, baseUrl, "")

    /* a spoofed forwarded address from an untrusted peer must not mint a fresh budget: every call below is counted against the loopback peer address, so the budget is spent once and stays spent */
    spentAt := 0
    for attempt := 1; attempt <= exampleRateLimitBudget+1; attempt++ {
        spoofed := fmt.Sprintf("203.0.113.%d", attempt)

        status := requestThrottledWrite(client, baseUrl, "", spoofed)
        if http.StatusTooManyRequests == status {
            spentAt = attempt
            break
        }
        if http.StatusUnauthorized == status || http.StatusForbidden == status {
            fail("example http: direct call %d returned %d — the section reached the firewall, not the limiter", attempt, status)
        }
    }

    if 0 == spentAt {
        fail(
            "example http: a spoofed X-Forwarded-For minted a fresh budget on every call — the untrusted peer's header was trusted",
        )
    }
    if exampleRateLimitBudget+1 != spentAt {
        fail("example http: the budget was exhausted at call %d, wanted %d", spentAt, exampleRateLimitBudget+1)
    }
    pass("example rate limit ignored a spoofed X-Forwarded-For from an untrusted peer (budget spent once)")

    /* the limiter keeps denying while the window stands */
    if http.StatusTooManyRequests != requestThrottledWrite(client, baseUrl, "", "198.51.100.7") {
        fail("example http: a new spoofed address was admitted after the budget was spent")
    }
    pass("example rate limit stayed closed for a new spoofed address inside the window")

    /* the budget is on what changes the catalogue, so browsing it must still answer */
    if http.StatusOK != requestExampleListing(client, baseUrl, "") {
        fail("example http: the product listing was refused while the write budget was spent — the limit reached the reads")
    }
    pass("example rate limit left the reads alone while the writes were refused")

    /* the conversion door rides the same signed-in client and the same spent budget on purpose: it is a
       READ, so the limiter that closed the writes has to leave it open, and driving it here says so without
       a section of its own */
    runExampleCurrencyConversionCheck(client, baseUrl)

    runExampleLoadBalancerCheck(client, loadBalancerUrl, redisAddress)
}

/* runExampleLoadBalancerCheck drives the load-balancer half — the ONLY place the trusted-proxy chain is proven: through the load balancer the peer is nginx (a trusted proxy), so the forwarded chain is honoured and the budget is enforced against the resolved client address.

   Because it is the sole proof of that property, an unset EXAMPLE_LOAD_BALANCER_URL must announce a SKIP rather than pass silently — otherwise a regression in the forwarded-chain resolution goes green (the same false-green class the REDIS_ADDRESS skip already guards against: the direct-path checks still pass, this assertion never runs, and the section is reported fully passed). */
func runExampleLoadBalancerCheck(client *http.Client, loadBalancerUrl string, redisAddress string) {
    if "" == loadBalancerUrl {
        skip("example http: EXAMPLE_LOAD_BALANCER_URL is not set — the load balancer trusted-proxy half did not run")
        return
    }

    resetExampleRateLimitCounters("example http", redisAddress, exampleRateLimitPrefix)

    /* the cookie jar keys sessions by the host of the url, so the session established against the direct address is not sent to the load balancer: this half signs in through the load balancer itself */
    signInExampleHttpEditor(client, loadBalancerUrl, exampleHostHeader)

    /* the key the budget charges through the balancer must NOT be the balancer's own address, and that is what separates the chain being honoured from its FALLBACK: with the balancer's name unresolved or stale the list is empty, no header is believed, and every client behind the balancer is charged to the balancer's own address — the peer the example sees. The counts alone (budget+1 in both arms below) read the same in both states, so a fallback would have been green. The balancer's addresses are read off the url's host, the way the example resolves the name it trusts. */
    balancerAddressList := balancerAddressesOf(loadBalancerUrl)

    resetExampleRateLimitCounters("example http", redisAddress, exampleRateLimitPrefix)

    balancerSpentAt := 0
    for attempt := 1; attempt <= exampleRateLimitBudget+1; attempt++ {
        status := requestThrottledWrite(client, loadBalancerUrl, exampleHostHeader, "")
        if http.StatusTooManyRequests == status {
            balancerSpentAt = attempt
            break
        }
        if http.StatusUnauthorized == status || http.StatusForbidden == status {
            fail("example http: load balancer call %d returned %d — the section reached the firewall, not the limiter", attempt, status)
        }
    }

    if exampleRateLimitBudget+1 != balancerSpentAt {
        fail(
            "example http: through the load balancer the budget was exhausted at call %d, wanted %d",
            balancerSpentAt,
            exampleRateLimitBudget+1,
        )
    }
    pass("example rate limit enforced the shared budget through the load balancer (429 past the budget)")

    balancerKeys := exampleRateLimitKeys("example http", redisAddress, exampleRateLimitPrefix)
    if 1 != len(balancerKeys) {
        fail("example http: expected the writes through the load balancer to charge exactly one key, found %v", balancerKeys)
    }
    for _, balancerAddress := range balancerAddressList {
        if strings.HasSuffix(balancerKeys[0], ":"+balancerAddress) {
            fail(
                "example http: through the load balancer the budget was charged to the balancer's own address %s (%v) — its attestation was not read (the trusted proxy list is empty or stale)",
                balancerAddress,
                balancerKeys,
            )
        }
    }
    pass("example rate limit charged the client the balancer attested, not the balancer itself (the trusted proxy list resolves)")

    /* the balancer APPENDS the peer it saw to the chain the client sent, so a client that sends its own X-Forwarded-For arrives as "<what it sent>, <its address>". With the whole private space trusted, the address the balancer appended — this harness's container, and the docker gateway for every host client — read as one more hop, and the client's own entry became the key: a fresh budget per call. Trusting the balancer alone, the appended address is the client the balancer attested, and what the client wrote to its left is never read. */
    resetExampleRateLimitCounters("example http", redisAddress, exampleRateLimitPrefix)

    balancerSpoofSpentAt := 0
    for attempt := 1; attempt <= exampleRateLimitBudget+1; attempt++ {
        status := requestThrottledWrite(client, loadBalancerUrl, exampleHostHeader, fmt.Sprintf("203.0.113.%d", attempt))
        if http.StatusTooManyRequests == status {
            balancerSpoofSpentAt = attempt
            break
        }
        if http.StatusUnauthorized == status || http.StatusForbidden == status {
            fail("example http: load balancer call %d with a spoofed header returned %d — the section reached the firewall, not the limiter", attempt, status)
        }
    }

    if exampleRateLimitBudget+1 != balancerSpoofSpentAt {
        fail(
            "example http: through the load balancer a spoofed X-Forwarded-For was believed — the budget was exhausted at call %d, wanted %d (the address the balancer appended was read as a trusted hop)",
            balancerSpoofSpentAt,
            exampleRateLimitBudget+1,
        )
    }
    pass("example rate limit ignored a spoofed X-Forwarded-For sent through the load balancer (budget spent once)")
}

/* exampleHostHeader is the virtual host the load balancer serves the example under. */
const exampleHostHeader = "example.melody.localhost.precision-soft.com"

/* newExampleHttpClient keeps a cookie jar so the session the sign-in establishes travels on every following call, exactly as a browser would send it. Redirects are not followed: a redirect answer to a write would otherwise be read as a success. */
func newExampleHttpClient() *http.Client {
    jar, jarErr := cookiejar.New(nil)
    if nil != jarErr {
        fail("example http: open a cookie jar: %v", jarErr)
    }

    return &http.Client{
        Timeout: 5 * time.Second,
        Jar:     jar,
        CheckRedirect: func(request *http.Request, previous []*http.Request) error {
            return http.ErrUseLastResponse
        },
    }
}

func signInExampleHttpEditor(client *http.Client, baseUrl string, hostHeader string) {
    signInExampleHttp(client, baseUrl, hostHeader, exampleHttpEditorUsername, exampleHttpEditorPassword)
}

/* the admin is what a section signs in as when it drives the directory: the user routes are behind ROLE_ADMIN, which the editor does not hold. */
func signInExampleHttpAdmin(client *http.Client, baseUrl string) {
    signInExampleHttp(client, baseUrl, "", exampleHttpAdminUsername, exampleHttpAdminPassword)
}

func signInExampleHttp(client *http.Client, baseUrl string, hostHeader string, username string, password string) {
    body := "username=" + username + "&password=" + password

    request, requestErr := http.NewRequest("POST", strings.TrimRight(baseUrl, "/")+"/login/", strings.NewReader(body))
    if nil != requestErr {
        fail("example http: build the sign-in request: %v", requestErr)
    }

    request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    request.Header.Set("Accept", "application/json")
    if "" != hostHeader {
        request.Host = hostHeader
    }

    response, responseErr := client.Do(request)
    if nil != responseErr {
        fail("example http: sign in: %v", responseErr)
    }
    defer response.Body.Close()

    _, _ = io.Copy(io.Discard, response.Body)

    if http.StatusOK != response.StatusCode {
        fail(
            "example http: the seeded editor could not sign in (%d), so the throttled write cannot be reached",
            response.StatusCode,
        )
    }
}

func requestThrottledWrite(client *http.Client, baseUrl string, hostHeader string, forwardedFor string) int {
    return requestExample(client, "POST", baseUrl, exampleThrottledWriteRoute, hostHeader, forwardedFor, exampleThrottledWriteBody)
}

func requestExampleListing(client *http.Client, baseUrl string, hostHeader string) int {
    return requestExample(client, "GET", baseUrl, "/products/api/read/", hostHeader, "", "")
}

func requestExample(client *http.Client, method string, baseUrl string, path string, hostHeader string, forwardedFor string, body string) int {
    var reader io.Reader
    if "" != body {
        reader = strings.NewReader(body)
    }

    request, requestErr := http.NewRequest(method, strings.TrimRight(baseUrl, "/")+path, reader)
    if nil != requestErr {
        fail("example http: build request: %v", requestErr)
    }

    if "" != body {
        request.Header.Set("Content-Type", "application/json")
    }

    request.Header.Set("Accept", "application/json")

    if "" != hostHeader {
        request.Host = hostHeader
    }
    if "" != forwardedFor {
        request.Header.Set("X-Forwarded-For", forwardedFor)
    }

    response, responseErr := client.Do(request)
    if nil != responseErr {
        fail("example http: %s: %v", request.URL, responseErr)
    }
    defer response.Body.Close()

    _, _ = io.Copy(io.Discard, response.Body)

    return response.StatusCode
}

/* resetExampleRateLimitCounters clears the counters one limiter wrote, so a section starts from a full budget instead of inheriting a spent one. The prefix is a parameter because the applications under test keep separate counters: they share one redis, and a section that measures an exact exhaustion point cannot have another application spending its budget. */
/* balancerAddressesOf resolves the host of the load balancer url to the addresses the example sees the balancer under; a host that does not resolve fails the section, because an assertion against no address would pass over anything. */
func balancerAddressesOf(loadBalancerUrl string) []string {
    parsed, parseErr := url.Parse(loadBalancerUrl)
    if nil != parseErr {
        fail("example http: parse the load balancer url %q: %v", loadBalancerUrl, parseErr)
    }

    addressList, lookupErr := net.LookupHost(parsed.Hostname())
    if nil != lookupErr || 0 == len(addressList) {
        fail("example http: the load balancer host %q resolves to nothing (%v)", parsed.Hostname(), lookupErr)
    }

    return addressList
}

/* exampleRateLimitKeys lists the keys the budget has charged, which name the client each was charged to. */
func exampleRateLimitKeys(label string, redisAddress string, prefix string) []string {
    if "" == redisAddress {
        fail("%s: REDIS_ADDRESS is required to read the rate limit counters", label)
    }

    client := openRedis(redisAddress)
    defer client.Close()

    keys, keysErr := client.Do(context.Background(), client.B().Keys().Pattern(prefix+"*").Build()).AsStrSlice()
    if nil != keysErr {
        fail("%s: list rate limit keys: %v", label, keysErr)
    }

    return keys
}

func resetExampleRateLimitCounters(label string, redisAddress string, prefix string) {
    if "" == redisAddress {
        fail("%s: REDIS_ADDRESS is required to clear the rate limit counters", label)
    }

    client := openRedis(redisAddress)
    defer client.Close()

    ctx := context.Background()

    keys, keysErr := client.Do(ctx, client.B().Keys().Pattern(prefix+"*").Build()).AsStrSlice()
    if nil != keysErr {
        fail("%s: list rate limit keys: %v", label, keysErr)
    }

    if 0 == len(keys) {
        return
    }

    if deleteErr := client.Do(ctx, client.B().Del().Key(keys...).Build()).Error(); nil != deleteErr {
        fail("%s: clear rate limit keys: %v", label, deleteErr)
    }
}
