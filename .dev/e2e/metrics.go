package main

import (
    "fmt"
    "net/http"
    "os"
    "regexp"
    "strconv"
    "strings"
    "time"
)

const metricsLabel = "metrics"

const metricsRoute = "/metrics"

/* metricsScrapeToken is the example's APP_METRICS_TOKEN (v3/.example/.env), the credential its development prometheus presents too: /metrics answers no caller without it */
const metricsScrapeToken = "melody-example-metrics-scrape-token-0001"

/* metricsSeriesSet names the exact exposition series the deltas are read from: handledCountName counts the requests the metrics middleware saw, lifecycleCountName the requests the handler decorator saw, short-circuited ones included, and durationCountName is the count series of the handled-request duration histogram. The names are matched whole, so one family never folds another's samples into a delta. */
type metricsSeriesSet struct {
    form               string
    typeLine           string
    handledCountName   string
    lifecycleCountName string
    durationCountName  string
}

/* metricsCounterSeriesSet is the exposition of the v3 integration: the exporter suffixes the "http.server.request.count" and "http.server.lifecycle.request.count" counters with "_total" and folds the millisecond unit into the "http.server.request.duration" histogram's name. */
var metricsCounterSeriesSet = metricsSeriesSet{
    form:               "the counter form",
    typeLine:           "# TYPE http_server_request_count_total counter",
    handledCountName:   "http_server_request_count_total",
    lifecycleCountName: "http_server_lifecycle_request_count_total",
    durationCountName:  "http_server_request_duration_milliseconds_count",
}

/* metricsHistogramSeriesSet is the exposition of the v4 integration: no counter, the "http.server.request.duration" and "melody.http.server.lifecycle.duration" histograms in seconds, each request one observation on the histogram's "_count" series, so the handled count and the duration count are the same series. */
var metricsHistogramSeriesSet = metricsSeriesSet{
    form:               "the histogram form",
    typeLine:           "# TYPE http_server_request_duration_seconds histogram",
    handledCountName:   "http_server_request_duration_seconds_count",
    lifecycleCountName: "melody_http_server_lifecycle_duration_seconds_count",
    durationCountName:  "http_server_request_duration_seconds_count",
}

/* metricsProbeRoute is the route this section calls, and metricsControlRoute is the route it must NOT touch. The route LABEL is the pattern the router matched, which carries no trailing slash. */
const (
    metricsProbeRoute        = "/health"
    metricsProbeRouteLabel   = "/health"
    metricsControlPath       = "/en/i18n/greeting/?count=1"
    metricsControlRouteLabel = "/:_locale/i18n/greeting"
)

/* metricsRefusedRoute is a route the firewall refuses an anonymous caller with 401 before any middleware runs, and metricsRefusedRequestCount is how many such requests the section issues: the lifecycle count must grow by exactly that many 401s while the middleware's 401 series stays where it was. */
const (
    metricsRefusedRoute        = "/categories/api/read/"
    metricsRefusedRequestCount = 2
)

/* metricsProbeRequestCount is how many probe requests the section issues; the count delta must equal it exactly. */
const metricsProbeRequestCount = 3

/* metricsScrapeBudget bounds the wait for prometheus to report the example as up. The dev scrape interval is 5s (.dev/docker/prometheus/prometheus.yml), so this covers several intervals without turning a down target into a hung run. */
const metricsScrapeBudget = 30 * time.Second

/* runMetricsCheck asserts the metrics pipeline end to end, and it runs LAST of the example-over-http group because its assertions are on EXACT deltas: any request issued by a later section against the same application would move a count this section already read.

   The delta assertions are the point. "The count exists" is satisfied by a middleware that recorded one request and then stopped, and "the count is above zero" is satisfied by requests from earlier sections. Growing by EXACTLY the number of requests issued is satisfied by neither, and the untouched control route is what separates "the count grows" from "the count grows for the route that was called". */
func runMetricsCheck(baseUrl string) {
    client := newLiveExampleClient(baseUrl)

    /* the exposition is the scraper's alone: a caller without the credential, or with another one, is refused 401 before the baseline is read with it, so these two refusals move no series the deltas below are taken over */
    for _, headerList := range []map[string]string{{}, {"Authorization": "Bearer not-the-scrape-token"}} {
        refused := client.call(metricsLabel, liveExampleRequest{method: "GET", path: metricsRoute, headerList: headerList})
        if http.StatusUnauthorized != refused.statusCode {
            fail("%s: %s answered %d without the scrape credential (%v), wanted 401", metricsLabel, metricsRoute, refused.statusCode, headerList)
        }
    }
    pass("%s refuses a caller without the scrape credential, or with another one, 401", metricsRoute)

    /* the control route is called ONCE here, before the baseline, purely so its series is guaranteed to exist: a control assertion comparing an absent series against an absent series proves nothing at all */
    client.get(metricsLabel, metricsControlPath)

    baseline := readMetricsExposition(client)

    assertMetricsExpositionFormat(baseline)

    seriesSet, seriesSetFound := selectMetricsSeriesSet(baseline)
    if false == seriesSetFound {
        fail(
            "%s: %s carries neither %q nor %q after the control route was called — the request instrumentation is not exposed in a form this section reads: %s",
            metricsLabel,
            metricsRoute,
            metricsHistogramSeriesSet.typeLine,
            metricsCounterSeriesSet.typeLine,
            exampleTruncate(baseline),
        )
    }
    pass("%s exposes the request instrumentation in %s (%s)", metricsRoute, seriesSet.form, seriesSet.handledCountName)

    probeBefore, probeBeforeFound := metricsRequestCount(baseline, seriesSet.handledCountName, metricsProbeRouteLabel)
    if false == probeBeforeFound {
        /* the probe route may legitimately have no series yet on a freshly restarted application, and zero is the right baseline for that case — but it must be reported, because it is also what a lost middleware looks like from here */
        skip("%s: the probe route %s had no count series before the probe requests (a freshly booted application)", metricsLabel, metricsProbeRouteLabel)
    }

    durationBefore, _ := metricsRequestCount(baseline, seriesSet.durationCountName, metricsProbeRouteLabel)

    controlBefore, controlBeforeFound := metricsRequestCount(baseline, seriesSet.handledCountName, metricsControlRouteLabel)
    if false == controlBeforeFound {
        fail(
            "%s: the control route %s has no count series even after being called once — the control assertion below could not distinguish an unchanged series from a missing one",
            metricsLabel,
            metricsControlRouteLabel,
        )
    }

    for issued := 1; issued <= metricsProbeRequestCount; issued++ {
        response := client.get(metricsLabel, metricsProbeRoute)
        requireLiveExampleStatus(metricsLabel, metricsProbeRoute, response, http.StatusOK)
    }

    lifecycleRefusedBefore, _ := metricsStatusCount(baseline, seriesSet.lifecycleCountName, "401")
    handledRefusedBefore, _ := metricsStatusCount(baseline, seriesSet.handledCountName, "401")

    for issued := 1; issued <= metricsRefusedRequestCount; issued++ {
        response := client.get(metricsLabel, metricsRefusedRoute)
        requireLiveExampleStatus(metricsLabel, metricsRefusedRoute, response, http.StatusUnauthorized)
    }

    final := readMetricsExposition(client)

    probeAfter, probeAfterFound := metricsRequestCount(final, seriesSet.handledCountName, metricsProbeRouteLabel)
    if false == probeAfterFound {
        fail(
            "%s: after %d requests to %s the request count has NO series for it — the metrics middleware recorded nothing",
            metricsLabel,
            metricsProbeRequestCount,
            metricsProbeRouteLabel,
        )
    }

    if metricsProbeRequestCount != int(probeAfter-probeBefore) {
        fail(
            "%s: the request count for %s grew by %g over %d requests, wanted exactly %d (%g -> %g)",
            metricsLabel,
            metricsProbeRouteLabel,
            probeAfter-probeBefore,
            metricsProbeRequestCount,
            metricsProbeRequestCount,
            probeBefore,
            probeAfter,
        )
    }
    pass("the request count for %s grew by exactly %d over %d requests", metricsProbeRouteLabel, metricsProbeRequestCount, metricsProbeRequestCount)

    durationAfter, durationAfterFound := metricsRequestCount(final, seriesSet.durationCountName, metricsProbeRouteLabel)
    if false == durationAfterFound {
        fail(
            "%s: after %d requests to %s the duration histogram has NO series for it — the count is recorded but the timing is not",
            metricsLabel,
            metricsProbeRequestCount,
            metricsProbeRouteLabel,
        )
    }
    if metricsProbeRequestCount != int(durationAfter-durationBefore) {
        fail(
            "%s: the duration histogram count for %s grew by %g, wanted exactly %d (%g -> %g)",
            metricsLabel,
            metricsProbeRouteLabel,
            durationAfter-durationBefore,
            metricsProbeRequestCount,
            durationBefore,
            durationAfter,
        )
    }
    pass("the duration histogram count for %s grew by exactly %d as well", metricsProbeRouteLabel, metricsProbeRequestCount)

    controlAfter, controlAfterFound := metricsRequestCount(final, seriesSet.handledCountName, metricsControlRouteLabel)
    if false == controlAfterFound {
        fail(
            "%s: the control route %s LOST its count series between the two scrapes — a series that disappears makes every delta above unreliable",
            metricsLabel,
            metricsControlRouteLabel,
        )
    }
    if controlBefore != controlAfter {
        fail(
            "%s: the request count for %s moved from %g to %g while only %s was called — requests are being attributed to the wrong route label",
            metricsLabel,
            metricsControlRouteLabel,
            controlBefore,
            controlAfter,
            metricsProbeRouteLabel,
        )
    }
    pass("the control route %s, which was not called, stayed at %g", metricsControlRouteLabel, controlAfter)

    lifecycleRefusedAfter, lifecycleRefusedFound := metricsStatusCount(final, seriesSet.lifecycleCountName, "401")
    if false == lifecycleRefusedFound || metricsRefusedRequestCount != int(lifecycleRefusedAfter-lifecycleRefusedBefore) {
        fail(
            "%s: the lifecycle count's 401 series grew from %g to %g (found %v) over %d refusals of %s, wanted exactly %d — the handler decorator is not counting the firewall's short-circuits",
            metricsLabel,
            lifecycleRefusedBefore,
            lifecycleRefusedAfter,
            lifecycleRefusedFound,
            metricsRefusedRequestCount,
            metricsRefusedRoute,
            metricsRefusedRequestCount,
        )
    }

    handledRefusedAfter, _ := metricsStatusCount(final, seriesSet.handledCountName, "401")
    if handledRefusedBefore != handledRefusedAfter {
        fail(
            "%s: the metrics middleware's 401 series moved from %g to %g over refusals the firewall answers before the middleware runs — the lifecycle count is no longer the only one that sees them",
            metricsLabel,
            handledRefusedBefore,
            handledRefusedAfter,
        )
    }
    pass("%d anonymous refusals of %s grew the lifecycle count's 401 series by exactly %d and left the middleware's 401 series at %g: only the decorator sees a security short-circuit", metricsRefusedRequestCount, metricsRefusedRoute, metricsRefusedRequestCount, handledRefusedAfter)

    assertPrometheusScrapesTheExample()
}

/* selectMetricsSeriesSet reads which form the exposition carries from its # TYPE line: the histogram form when the handled-request histogram is in seconds, the counter form when the handled-request counter is present, and nothing found when neither is. */
func selectMetricsSeriesSet(body string) (metricsSeriesSet, bool) {
    for _, seriesSet := range []metricsSeriesSet{metricsHistogramSeriesSet, metricsCounterSeriesSet} {
        for _, line := range strings.Split(body, "\n") {
            if seriesSet.typeLine == strings.TrimSpace(line) {
                return seriesSet, true
            }
        }
    }

    return metricsSeriesSet{}, false
}

/* metricsStatusCount sums every GET series named seriesName answered with statusCode, whatever its route: the lifecycle family carries no route label, and the middleware's 401s are read across every route. */
func metricsStatusCount(body string, seriesName string, statusCode string) (float64, bool) {
    return prometheusSeriesSum(body, seriesName, map[string]string{
        "http_request_method":       "GET",
        "http_response_status_code": statusCode,
    })
}

func readMetricsExposition(client *liveExampleClient) string {
    response := client.call(metricsLabel, liveExampleRequest{
        method:     "GET",
        path:       metricsRoute,
        headerList: map[string]string{"Authorization": "Bearer " + metricsScrapeToken},
    })
    requireLiveExampleStatus(metricsLabel, metricsRoute, response, http.StatusOK)

    return response.bodyText()
}

/* assertMetricsExpositionFormat checks the body is prometheus exposition and not, say, a json envelope or an html error page served with a 200 — every parse below would then simply find no series and the deltas would be reported as zero growth. */
func assertMetricsExpositionFormat(body string) {
    if false == strings.Contains(body, "# HELP ") || false == strings.Contains(body, "# TYPE ") {
        fail(
            "%s: %s did not answer prometheus exposition (no # HELP / # TYPE lines): %s",
            metricsLabel,
            metricsRoute,
            exampleTruncate(body),
        )
    }

    pass("%s answered a prometheus exposition body (%d bytes)", metricsRoute, len(body))
}

/* metricsRequestCount sums the GET 200 series named seriesName recorded under routeLabel. */
func metricsRequestCount(body string, seriesName string, routeLabel string) (float64, bool) {
    return prometheusSeriesSum(body, seriesName, map[string]string{
        "http_route":                routeLabel,
        "http_request_method":       "GET",
        "http_response_status_code": "200",
    })
}

/* prometheusLabelPattern matches one label pair of an exposition line, tolerating a backslash escape inside the value so a label carrying a quote or a comma cannot end the match early. */
var prometheusLabelPattern = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*)="((?:[^"\\]|\\.)*)"`)

/* prometheusSeriesSum sums the value of every series whose metric name is seriesName and which carries all of the requested labels with the requested values. Extra labels are ignored: the otel exporter attaches otel_scope_* labels to everything, and a parser that demanded an exact label set would match nothing.

   The second return value is what makes the control assertion meaningful: a MISSING series and a series whose value is zero are different findings — the first means nothing was ever recorded under that label, the second means a value is recorded and is genuinely at zero — and a parser that collapsed them into 0 would let "the control route stayed unchanged" pass on a scrape where the whole metric had disappeared. A matched line whose value does not parse as a number contributes nothing and does not count as found, which keeps a malformed exposition on the safe side of that distinction rather than silently reporting a wrong number. */
func prometheusSeriesSum(body string, seriesName string, labelList map[string]string) (float64, bool) {
    total := 0.0
    found := false

    for _, line := range strings.Split(body, "\n") {
        trimmed := strings.TrimSpace(line)
        if "" == trimmed || true == strings.HasPrefix(trimmed, "#") {
            continue
        }

        name, labelBlock, value, parsed := splitPrometheusLine(trimmed)
        if false == parsed {
            continue
        }

        if seriesName != name {
            continue
        }

        if false == prometheusLabelsMatch(labelBlock, labelList) {
            continue
        }

        number, convertErr := strconv.ParseFloat(value, 64)
        if nil != convertErr {
            continue
        }

        total += number
        found = true
    }

    return total, found
}

/* splitPrometheusLine cuts one sample line into its metric name, its label block and its value. The label block is delimited by the FIRST '{' and the LAST '}' before the value, because a label value may itself contain a brace. */
func splitPrometheusLine(line string) (string, string, string, bool) {
    open := strings.Index(line, "{")
    if -1 == open {
        fields := strings.Fields(line)
        if 2 > len(fields) {
            return "", "", "", false
        }

        return fields[0], "", fields[1], true
    }

    closeIndex := strings.LastIndex(line, "}")
    if closeIndex < open {
        return "", "", "", false
    }

    fields := strings.Fields(strings.TrimSpace(line[closeIndex+1:]))
    if 0 == len(fields) {
        return "", "", "", false
    }

    return line[:open], line[open+1 : closeIndex], fields[0], true
}

func prometheusLabelsMatch(labelBlock string, labelList map[string]string) bool {
    present := map[string]string{}

    for _, match := range prometheusLabelPattern.FindAllStringSubmatch(labelBlock, -1) {
        present[match[1]] = match[2]
    }

    for name, expected := range labelList {
        if expected != present[name] {
            return false
        }
    }

    return true
}

/* assertPrometheusScrapesTheExample closes the last link of the pipeline: the integration records, /metrics exposes, and a real prometheus SCRAPES it. Without this half the section proves the endpoint answers and says nothing about whether anything can collect from it — a body that is valid exposition but served with the wrong content type, or an endpoint the scrape config points somewhere else, both go unnoticed. */
func assertPrometheusScrapesTheExample() {
    prometheusUrl := os.Getenv("PROMETHEUS_URL")
    if "" == prometheusUrl {
        skip("%s: PROMETHEUS_URL is not set — the scrape half did NOT run, so nothing here proves a collector can read the endpoint", metricsLabel)

        return
    }

    client := &http.Client{Timeout: 5 * time.Second}
    query := strings.TrimRight(prometheusUrl, "/") + `/api/v1/query?query=up%7Bjob%3D%22melody-example%22%7D`

    deadline := time.Now().Add(metricsScrapeBudget)
    lastSeen := "no answer at all"

    for time.Now().Before(deadline) {
        response, responseErr := client.Get(query)
        if nil != responseErr {
            lastSeen = fmt.Sprintf("query failed: %v", responseErr)

            time.Sleep(time.Second)

            continue
        }

        body := make([]byte, 8192)
        read, _ := response.Body.Read(body)
        _ = response.Body.Close()

        answer := string(body[:read])
        lastSeen = exampleTruncate(answer)

        /* the value the query returns is the target's up state; "1" is the only value that means the scrape succeeded, and an empty result set means prometheus does not know the target at all */
        if true == strings.Contains(answer, `"job":"melody-example"`) && true == strings.Contains(answer, `"1"`) {
            pass("prometheus reports the example target up (a real collector is scraping %s)", metricsRoute)

            return
        }

        time.Sleep(time.Second)
    }

    fail(
        "%s: prometheus did not report up{job=\"melody-example\"} = 1 within %s — the last answer was: %s",
        metricsLabel,
        metricsScrapeBudget,
        lastSeen,
    )
}
