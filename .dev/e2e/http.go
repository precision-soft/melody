package main

import (
    "context"
    "fmt"
    "io"
    "net/http"
    "strings"
    "time"
)

/* exampleRateLimitBudget mirrors the limit the example wires on /ratelimit/demo (5 per minute). */
const exampleRateLimitBudget = 5

/* exampleRateLimitPrefix mirrors the key prefix the example gives its redis rate limiter, so the harness can
clear the counters of a previous run rather than inherit a spent budget inside the same fixed window. */
const exampleRateLimitPrefix = "melody-example:rate_limit:"

/* runExampleHttpCheck drives the running .example application over real HTTP — the only place the whole
chain is exercised together: nginx sets X-Forwarded-For, the forwarded-client-ip resolver decides which hop
to trust, and the distributed rate limiter counts against the resolved key in redis.

Two properties matter and neither can be proven in-process:

  - through the load balancer the budget is enforced across requests and the request past it gets a 429;
  - straight to the application from an UNTRUSTED peer (loopback is outside the example's trusted proxy
    list) a spoofed X-Forwarded-For is ignored, so an attacker cannot mint a fresh budget per fake address.

The section runs when EXAMPLE_BASE_URL is set and needs REDIS_ADDRESS to clear the counters first. */
func runExampleHttpCheck(baseUrl string, loadBalancerUrl string, redisAddress string) {
    resetExampleRateLimitCounters(redisAddress)

    client := &http.Client{Timeout: 5 * time.Second}

    /* a spoofed forwarded address from an untrusted peer must not mint a fresh budget: every call below is
       counted against the loopback peer address, so the budget is spent once and stays spent */
    spentAt := 0
    for attempt := 1; attempt <= exampleRateLimitBudget+1; attempt++ {
        spoofed := fmt.Sprintf("203.0.113.%d", attempt)

        status := requestRateLimitDemo(client, baseUrl, "", spoofed)
        if http.StatusTooManyRequests == status {
            spentAt = attempt
            break
        }
        if http.StatusOK != status {
            fail("example http: direct call %d returned %d, wanted 200", attempt, status)
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
    if http.StatusTooManyRequests != requestRateLimitDemo(client, baseUrl, "", "198.51.100.7") {
        fail("example http: a new spoofed address was admitted after the budget was spent")
    }
    pass("example rate limit stayed closed for a new spoofed address inside the window")

    if "" == loadBalancerUrl {
        return
    }

    /* through the load balancer the peer is nginx — a trusted proxy — so the forwarded chain is honoured and
       the budget is enforced against the resolved client address */
    resetExampleRateLimitCounters(redisAddress)

    balancerSpentAt := 0
    for attempt := 1; attempt <= exampleRateLimitBudget+1; attempt++ {
        status := requestRateLimitDemo(client, loadBalancerUrl, exampleHostHeader, "")
        if http.StatusTooManyRequests == status {
            balancerSpentAt = attempt
            break
        }
        if http.StatusOK != status {
            fail("example http: load balancer call %d returned %d, wanted 200", attempt, status)
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
}

/* exampleHostHeader is the virtual host the load balancer serves the example under. */
const exampleHostHeader = "example.melody.localhost.precision-soft.com"

func requestRateLimitDemo(client *http.Client, baseUrl string, hostHeader string, forwardedFor string) int {
    request, requestErr := http.NewRequest("GET", strings.TrimRight(baseUrl, "/")+"/ratelimit/demo", nil)
    if nil != requestErr {
        fail("example http: build request: %v", requestErr)
    }

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

/* resetExampleRateLimitCounters clears the counters the example's limiter wrote, so a re-run inside the same
fixed window starts from a full budget instead of inheriting a spent one. */
func resetExampleRateLimitCounters(redisAddress string) {
    if "" == redisAddress {
        fail("example http: REDIS_ADDRESS is required to clear the example rate limit counters")
    }

    client := openRedis(redisAddress)
    defer client.Close()

    ctx := context.Background()

    keys, keysErr := client.Do(ctx, client.B().Keys().Pattern(exampleRateLimitPrefix+"*").Build()).AsStrSlice()
    if nil != keysErr {
        fail("example http: list rate limit keys: %v", keysErr)
    }

    if 0 == len(keys) {
        return
    }

    if deleteErr := client.Do(ctx, client.B().Del().Key(keys...).Build()).Error(); nil != deleteErr {
        fail("example http: clear rate limit keys: %v", deleteErr)
    }
}
