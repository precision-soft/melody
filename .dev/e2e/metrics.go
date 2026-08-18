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

/* the metric names are matched by PREFIX rather than by their exact sanitised spelling. The opentelemetry
instruments are declared as "http.server.request.count" and "http.server.request.duration" and the prometheus
exporter rewrites both — dots to underscores, a "_total" suffix on the counter, the unit folded into the histogram's
name ("http_server_request_duration_milliseconds"). Pinning the rewritten spelling would make this section fail on
an exporter upgrade that changed a suffix while the metric itself kept working; the integration's own test matches
by prefix for the same reason. */
const (
    metricsCounterPrefix        = "http_server_request_count"
    metricsCounterSuffix        = "_total"
    metricsHistogramPrefix      = "http_server_request_duration"
    metricsHistogramCountSuffix = "_count"
)

/* metricsProbeRoute is the route this section calls, and metricsControlRoute is the route it must NOT touch. The
route LABEL is the pattern the router matched, which carries no trailing slash. */
const (
    metricsProbeRoute        = "/health"
    metricsProbeRouteLabel   = "/health"
    metricsControlPath       = "/i18n/greeting/?locale=en&count=1"
    metricsControlRouteLabel = "/i18n/greeting"
)

/* metricsProbeRequestCount is how many probe requests the section issues; the counter delta must equal it exactly. */
const metricsProbeRequestCount = 3

/* metricsScrapeBudget bounds the wait for prometheus to report the example as up. The dev scrape interval is 5s
(.dev/docker/prometheus/prometheus.yml), so this covers several intervals without turning a down target into a hung
run. */
const metricsScrapeBudget = 30 * time.Second

/* runMetricsCheck asserts the metrics pipeline end to end, and it runs LAST of the example-over-http group because
its assertions are on EXACT deltas: any request issued by a later section against the same application would move a
counter this section already measured.

The delta assertions are the point. "The counter exists" is satisfied by a middleware that recorded one request and
then stopped, and "the counter is above zero" is satisfied by requests from earlier sections. Growing by EXACTLY the
number of requests issued is satisfied by neither, and the untouched control route is what separates "the counter
grows" from "the counter grows for the route that was called". */
func runMetricsCheck(baseUrl string) {
    client := newLiveExampleClient(baseUrl)

    /* the control route is called ONCE here, before the baseline, purely so its series is guaranteed to exist: a
       control assertion comparing an absent series against an absent series proves nothing at all */
    client.get(metricsLabel, metricsControlPath)

    baseline := readMetricsExposition(client)

    assertMetricsExpositionFormat(baseline)

    probeBefore, probeBeforeFound := metricsRequestCount(baseline, metricsProbeRouteLabel)
    if false == probeBeforeFound {
        /* the probe route may legitimately have no series yet on a freshly restarted application, and zero is the
           right baseline for that case — but it must be reported, because it is also what a lost middleware looks
           like from here */
        skip("%s: the probe route %s had no counter series before the probe requests (a freshly booted application)", metricsLabel, metricsProbeRouteLabel)
    }

    durationBefore, _ := metricsRequestDurationCount(baseline, metricsProbeRouteLabel)

    controlBefore, controlBeforeFound := metricsRequestCount(baseline, metricsControlRouteLabel)
    if false == controlBeforeFound {
        fail(
            "%s: the control route %s has no counter series even after being called once — the control assertion below could not distinguish an unchanged series from a missing one",
            metricsLabel,
            metricsControlRouteLabel,
        )
    }

    for issued := 1; issued <= metricsProbeRequestCount; issued++ {
        response := client.get(metricsLabel, metricsProbeRoute)
        requireLiveExampleStatus(metricsLabel, metricsProbeRoute, response, http.StatusOK)
    }

    final := readMetricsExposition(client)

    probeAfter, probeAfterFound := metricsRequestCount(final, metricsProbeRouteLabel)
    if false == probeAfterFound {
        fail(
            "%s: after %d requests to %s the counter has NO series for it — the metrics middleware recorded nothing",
            metricsLabel,
            metricsProbeRequestCount,
            metricsProbeRouteLabel,
        )
    }

    if metricsProbeRequestCount != int(probeAfter-probeBefore) {
        fail(
            "%s: the counter for %s grew by %g over %d requests, wanted exactly %d (%g -> %g)",
            metricsLabel,
            metricsProbeRouteLabel,
            probeAfter-probeBefore,
            metricsProbeRequestCount,
            metricsProbeRequestCount,
            probeBefore,
            probeAfter,
        )
    }
    pass("the request counter for %s grew by exactly %d over %d requests", metricsProbeRouteLabel, metricsProbeRequestCount, metricsProbeRequestCount)

    durationAfter, durationAfterFound := metricsRequestDurationCount(final, metricsProbeRouteLabel)
    if false == durationAfterFound {
        fail(
            "%s: after %d requests to %s the duration histogram has NO series for it — the counter is recorded but the timing is not",
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

    controlAfter, controlAfterFound := metricsRequestCount(final, metricsControlRouteLabel)
    if false == controlAfterFound {
        fail(
            "%s: the control route %s LOST its counter series between the two scrapes — a series that disappears makes every delta above unreliable",
            metricsLabel,
            metricsControlRouteLabel,
        )
    }
    if controlBefore != controlAfter {
        fail(
            "%s: the counter for %s moved from %g to %g while only %s was called — requests are being attributed to the wrong route label",
            metricsLabel,
            metricsControlRouteLabel,
            controlBefore,
            controlAfter,
            metricsProbeRouteLabel,
        )
    }
    pass("the control route %s, which was not called, stayed at %g", metricsControlRouteLabel, controlAfter)

    assertPrometheusScrapesTheExample()
}

func readMetricsExposition(client *liveExampleClient) string {
    response := client.get(metricsLabel, metricsRoute)
    requireLiveExampleStatus(metricsLabel, metricsRoute, response, http.StatusOK)

    return response.bodyText()
}

/* assertMetricsExpositionFormat checks the body is prometheus exposition and not, say, a json envelope or an html
error page served with a 200 — every parse below would then simply find no series and the deltas would be reported
as zero growth. */
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

func metricsRequestCount(body string, routeLabel string) (float64, bool) {
    return prometheusSeriesSum(body, metricsCounterPrefix, metricsCounterSuffix, map[string]string{
        "http_route":                routeLabel,
        "http_request_method":       "GET",
        "http_response_status_code": "200",
    })
}

func metricsRequestDurationCount(body string, routeLabel string) (float64, bool) {
    return prometheusSeriesSum(body, metricsHistogramPrefix, metricsHistogramCountSuffix, map[string]string{
        "http_route":                routeLabel,
        "http_request_method":       "GET",
        "http_response_status_code": "200",
    })
}

/* prometheusLabelPattern matches one label pair of an exposition line, tolerating a backslash escape inside the
value so a label carrying a quote or a comma cannot end the match early. */
var prometheusLabelPattern = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*)="((?:[^"\\]|\\.)*)"`)

/* prometheusSeriesSum sums the value of every series whose metric name starts with namePrefix, ends with
nameSuffix, and carries all of the requested labels with the requested values. Extra labels are ignored: the otel
exporter attaches otel_scope_* labels to everything, and a parser that demanded an exact label set would match
nothing.

The second return value is what makes the control assertion meaningful: a MISSING series and a series whose value is
zero are different findings — the first means nothing was ever recorded under that label, the second means it was
recorded and is genuinely at zero — and a parser that collapsed them into 0 would let "the control route stayed
unchanged" pass on a scrape where the whole metric had disappeared. A matched line whose value does not parse as a
number contributes nothing and does not count as found, which keeps a malformed exposition on the safe side of that
distinction rather than silently reporting a wrong number. */
func prometheusSeriesSum(body string, namePrefix string, nameSuffix string, labelList map[string]string) (float64, bool) {
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

        if false == strings.HasPrefix(name, namePrefix) || false == strings.HasSuffix(name, nameSuffix) {
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

/* splitPrometheusLine cuts one sample line into its metric name, its label block and its value. The label block is
delimited by the FIRST '{' and the LAST '}' before the value, because a label value may itself contain a brace. */
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

/* assertPrometheusScrapesTheExample closes the last link of the pipeline: the integration records, /metrics
exposes, and a real prometheus SCRAPES it. Without this half the section proves the endpoint answers and says
nothing about whether anything can collect from it — a body that is valid exposition but served with the wrong
content type, or an endpoint the scrape config points somewhere else, both go unnoticed. */
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

        /* the value the query returns is the target's up state; "1" is the only value that means the scrape
           succeeded, and an empty result set means prometheus does not know the target at all */
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
