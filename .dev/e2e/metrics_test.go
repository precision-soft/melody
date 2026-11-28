package main

import (
    "strings"
    "testing"
)

/* metricsTestExposition is a trimmed excerpt of what the v4 example's /metrics answers, label order, otel scope labels, url_scheme and the "le" label last on a bucket as the exporter writes them: the handled-request histogram and the lifecycle histogram in seconds, each with its _bucket, _sum and _count series, and no counter. */
const metricsTestExposition = `# HELP http_server_request_duration_seconds duration of handled http requests
# TYPE http_server_request_duration_seconds histogram
http_server_request_duration_seconds_bucket{http_request_method="GET",http_response_status_code="200",http_route="/health",otel_scope_name="melody.example",otel_scope_schema_url="",otel_scope_version="",url_scheme="http",le="0.005"} 0
http_server_request_duration_seconds_bucket{http_request_method="GET",http_response_status_code="200",http_route="/health",otel_scope_name="melody.example",otel_scope_schema_url="",otel_scope_version="",url_scheme="http",le="0.01"} 9
http_server_request_duration_seconds_bucket{http_request_method="GET",http_response_status_code="200",http_route="/health",otel_scope_name="melody.example",otel_scope_schema_url="",otel_scope_version="",url_scheme="http",le="+Inf"} 12
http_server_request_duration_seconds_sum{http_request_method="GET",http_response_status_code="200",http_route="/health",otel_scope_name="melody.example",otel_scope_schema_url="",otel_scope_version="",url_scheme="http"} 0.0415
http_server_request_duration_seconds_count{http_request_method="GET",http_response_status_code="200",http_route="/health",otel_scope_name="melody.example",otel_scope_schema_url="",otel_scope_version="",url_scheme="http"} 12
http_server_request_duration_seconds_bucket{http_request_method="GET",http_response_status_code="200",http_route="/:_locale/i18n/greeting",otel_scope_name="melody.example",otel_scope_schema_url="",otel_scope_version="",url_scheme="http",le="+Inf"} 1
http_server_request_duration_seconds_sum{http_request_method="GET",http_response_status_code="200",http_route="/:_locale/i18n/greeting",otel_scope_name="melody.example",otel_scope_schema_url="",otel_scope_version="",url_scheme="http"} 0.0021
http_server_request_duration_seconds_count{http_request_method="GET",http_response_status_code="200",http_route="/:_locale/i18n/greeting",otel_scope_name="melody.example",otel_scope_schema_url="",otel_scope_version="",url_scheme="http"} 1
http_server_request_duration_seconds_bucket{http_request_method="GET",http_response_status_code="401",http_route="/account",otel_scope_name="melody.example",otel_scope_schema_url="",otel_scope_version="",url_scheme="http",le="+Inf"} 5
http_server_request_duration_seconds_sum{http_request_method="GET",http_response_status_code="401",http_route="/account",otel_scope_name="melody.example",otel_scope_schema_url="",otel_scope_version="",url_scheme="http"} 0.0102
http_server_request_duration_seconds_count{http_request_method="GET",http_response_status_code="401",http_route="/account",otel_scope_name="melody.example",otel_scope_schema_url="",otel_scope_version="",url_scheme="http"} 5
# HELP melody_http_server_lifecycle_duration_seconds duration of http requests observed at the lifecycle seam, including short-circuited ones
# TYPE melody_http_server_lifecycle_duration_seconds histogram
melody_http_server_lifecycle_duration_seconds_bucket{http_request_method="GET",http_response_status_code="200",otel_scope_name="melody.example",otel_scope_schema_url="",otel_scope_version="",url_scheme="http",le="+Inf"} 14
melody_http_server_lifecycle_duration_seconds_sum{http_request_method="GET",http_response_status_code="200",otel_scope_name="melody.example",otel_scope_schema_url="",otel_scope_version="",url_scheme="http"} 0.0503
melody_http_server_lifecycle_duration_seconds_count{http_request_method="GET",http_response_status_code="200",otel_scope_name="melody.example",otel_scope_schema_url="",otel_scope_version="",url_scheme="http"} 14
melody_http_server_lifecycle_duration_seconds_bucket{http_request_method="GET",http_response_status_code="401",otel_scope_name="melody.example",otel_scope_schema_url="",otel_scope_version="",url_scheme="http",le="+Inf"} 7
melody_http_server_lifecycle_duration_seconds_sum{http_request_method="GET",http_response_status_code="401",otel_scope_name="melody.example",otel_scope_schema_url="",otel_scope_version="",url_scheme="http"} 0.0049
melody_http_server_lifecycle_duration_seconds_count{http_request_method="GET",http_response_status_code="401",otel_scope_name="melody.example",otel_scope_schema_url="",otel_scope_version="",url_scheme="http"} 7
# HELP target_info Target metadata
# TYPE target_info gauge
target_info{service_name="unknown_service:.example",telemetry_sdk_language="go",telemetry_sdk_name="opentelemetry",telemetry_sdk_version="1.45.0"} 1
`

/* metricsTestCounterExposition is a trimmed excerpt of what the v3 example's /metrics answers: the two counters with the exporter's _total suffix beside the millisecond histogram. */
const metricsTestCounterExposition = `# HELP http_server_lifecycle_request_count_total number of http requests observed at the lifecycle seam, including short-circuited ones
# TYPE http_server_lifecycle_request_count_total counter
http_server_lifecycle_request_count_total{http_request_method="GET",http_response_status_code="401",otel_scope_name="melody.example",otel_scope_schema_url="",otel_scope_version=""} 3
# HELP http_server_request_count_total number of handled http requests
# TYPE http_server_request_count_total counter
http_server_request_count_total{http_request_method="GET",http_response_status_code="200",http_route="/health",otel_scope_name="melody.example",otel_scope_schema_url="",otel_scope_version=""} 8
# HELP http_server_request_duration_milliseconds duration of handled http requests in milliseconds
# TYPE http_server_request_duration_milliseconds histogram
http_server_request_duration_milliseconds_bucket{http_request_method="GET",http_response_status_code="200",http_route="/health",otel_scope_name="melody.example",otel_scope_schema_url="",otel_scope_version="",le="5"} 8
http_server_request_duration_milliseconds_sum{http_request_method="GET",http_response_status_code="200",http_route="/health",otel_scope_name="melody.example",otel_scope_schema_url="",otel_scope_version=""} 17.5
http_server_request_duration_milliseconds_count{http_request_method="GET",http_response_status_code="200",http_route="/health",otel_scope_name="melody.example",otel_scope_schema_url="",otel_scope_version=""} 8
`

func TestSelectMetricsSeriesSet_ReadsTheHistogramFormOfTheV4Exposition(t *testing.T) {
    seriesSet, found := selectMetricsSeriesSet(metricsTestExposition)

    if false == found || "http_server_request_duration_seconds_count" != seriesSet.handledCountName {
        t.Fatalf("the v4 exposition selected %+v (found=%v), wanted the histogram form", seriesSet, found)
    }
    if "melody_http_server_lifecycle_duration_seconds_count" != seriesSet.lifecycleCountName || "http_server_request_duration_seconds_count" != seriesSet.durationCountName {
        t.Fatalf("the histogram form names %+v", seriesSet)
    }
}

func TestSelectMetricsSeriesSet_ReadsTheCounterFormOfTheV3Exposition(t *testing.T) {
    seriesSet, found := selectMetricsSeriesSet(metricsTestCounterExposition)

    if false == found || "http_server_request_count_total" != seriesSet.handledCountName {
        t.Fatalf("the v3 exposition selected %+v (found=%v), wanted the counter form", seriesSet, found)
    }
    if "http_server_lifecycle_request_count_total" != seriesSet.lifecycleCountName || "http_server_request_duration_milliseconds_count" != seriesSet.durationCountName {
        t.Fatalf("the counter form names %+v", seriesSet)
    }

    handled, handledFound := metricsRequestCount(metricsTestCounterExposition, seriesSet.handledCountName, "/health")
    duration, durationFound := metricsRequestCount(metricsTestCounterExposition, seriesSet.durationCountName, "/health")
    lifecycleRefused, lifecycleRefusedFound := metricsStatusCount(metricsTestCounterExposition, seriesSet.lifecycleCountName, "401")
    if false == handledFound || 8 != handled || false == durationFound || 8 != duration || false == lifecycleRefusedFound || 3 != lifecycleRefused {
        t.Fatalf("the counter form read handled %g (%v), duration %g (%v), lifecycle 401 %g (%v), wanted 8, 8 and 3", handled, handledFound, duration, durationFound, lifecycleRefused, lifecycleRefusedFound)
    }
}

/* the exposition of an application that served no request yet carries target_info alone: no form is selected, so the section fails naming both rather than reading every delta as zero. */
func TestSelectMetricsSeriesSet_SelectsNothingWithoutTheRequestInstrumentation(t *testing.T) {
    body := "# HELP target_info Target metadata\n# TYPE target_info gauge\ntarget_info{service_name=\"unknown_service:.example\"} 1\n"

    if seriesSet, found := selectMetricsSeriesSet(body); true == found {
        t.Fatalf("an exposition without the request instrumentation selected %+v", seriesSet)
    }
}

func TestPrometheusSeriesSum_ReadsTheRequestedSeries(t *testing.T) {
    value, found := metricsRequestCount(metricsTestExposition, metricsHistogramSeriesSet.handledCountName, "/health")

    if false == found {
        t.Fatal("the /health GET 200 count series must be found")
    }
    if 12 != value {
        t.Fatalf("the count read as %g, wanted 12", value)
    }
}

/* the whole control assertion rests on this: a series that is ABSENT and a series that is present at zero are different findings, and a parser that reported both as 0 would let "the control route stayed unchanged" pass on a scrape where the metric had vanished entirely. */
func TestPrometheusSeriesSum_DistinguishesMissingFromZero(t *testing.T) {
    zero, zeroFound := prometheusSeriesSum(metricsTestExposition, "http_server_request_duration_seconds_bucket", map[string]string{
        "http_route": "/health",
        "le":         "0.005",
    })
    if false == zeroFound {
        t.Fatal("a series present with the value 0 must be reported as FOUND")
    }
    if 0 != zero {
        t.Fatalf("the zero series read as %g", zero)
    }

    missing, missingFound := metricsRequestCount(metricsTestExposition, metricsHistogramSeriesSet.handledCountName, "/never-called")
    if true == missingFound {
        t.Fatal("a series that does not appear at all must be reported as NOT found")
    }
    if 0 != missing {
        t.Fatalf("a missing series must read as 0, got %g", missing)
    }
}

/* the labels select the series and the name selects the family: the middleware's 401 count is the handled histogram's alone, the lifecycle histogram's 401s are read from their own family, and neither folds the other in. */
func TestPrometheusSeriesSum_LabelsSelectTheSeries(t *testing.T) {
    handledRefused, handledRefusedFound := metricsStatusCount(metricsTestExposition, metricsHistogramSeriesSet.handledCountName, "401")
    if false == handledRefusedFound || 5 != handledRefused {
        t.Fatalf("the handled 401 count read as %g (found=%v), wanted 5", handledRefused, handledRefusedFound)
    }

    lifecycleRefused, lifecycleRefusedFound := metricsStatusCount(metricsTestExposition, metricsHistogramSeriesSet.lifecycleCountName, "401")
    if false == lifecycleRefusedFound || 7 != lifecycleRefused {
        t.Fatalf("the lifecycle 401 count read as %g (found=%v), wanted 7", lifecycleRefused, lifecycleRefusedFound)
    }
}

/* the histogram exposes three series under one family name; matching the whole series name keeps the _bucket and _sum samples, which carry the same labels, out of a count read across every route. */
func TestPrometheusSeriesSum_TheWholeNameSeparatesTheHistogramSeries(t *testing.T) {
    handled, handledFound := metricsStatusCount(metricsTestExposition, metricsHistogramSeriesSet.handledCountName, "200")
    if false == handledFound || 13 != handled {
        t.Fatalf("the handled 200 count across routes read as %g (found=%v), wanted 13", handled, handledFound)
    }

    sum, sumFound := prometheusSeriesSum(metricsTestExposition, "http_server_request_duration_seconds_sum", map[string]string{
        "http_route": "/health",
    })
    if false == sumFound || 0.0415 != sum {
        t.Fatalf("the histogram _sum read as %g (found=%v), wanted 0.0415", sum, sumFound)
    }

    lifecycle, lifecycleFound := metricsStatusCount(metricsTestExposition, metricsHistogramSeriesSet.lifecycleCountName, "200")
    if false == lifecycleFound || 14 != lifecycle {
        t.Fatalf("the lifecycle 200 count read as %g (found=%v), wanted 14", lifecycle, lifecycleFound)
    }
}

/* comment lines carry the metric name too, so a parser that did not skip them would read "# HELP" as a sample and report a bogus value or, worse, count a series that has no samples at all as found. */
func TestPrometheusSeriesSum_SkipsHelpAndTypeLines(t *testing.T) {
    body := "# HELP http_server_request_duration_seconds_count duration of handled http requests\n# TYPE http_server_request_duration_seconds_count histogram\n"

    if _, found := prometheusSeriesSum(body, metricsHistogramSeriesSet.handledCountName, map[string]string{}); true == found {
        t.Fatal("a body holding only # HELP / # TYPE lines carries no series")
    }
}

func TestSplitPrometheusLine_ReadsNameLabelsAndValue(t *testing.T) {
    name, labelBlock, value, parsed := splitPrometheusLine(`http_server_request_duration_seconds_count{http_route="/health"} 12`)

    if false == parsed {
        t.Fatal("a well-formed sample line must parse")
    }
    if "http_server_request_duration_seconds_count" != name {
        t.Fatalf("name parsed as %q", name)
    }
    if `http_route="/health"` != labelBlock {
        t.Fatalf("label block parsed as %q", labelBlock)
    }
    if "12" != value {
        t.Fatalf("value parsed as %q", value)
    }
}

/* a label value may itself hold a brace, so the block has to end at the LAST closing brace before the value rather than at the first one — otherwise the value is read out of the middle of the label block. */
func TestSplitPrometheusLine_TakesTheLastClosingBrace(t *testing.T) {
    _, labelBlock, value, parsed := splitPrometheusLine(`metric{path="/a{b}",kind="x"} 3`)

    if false == parsed {
        t.Fatal("a line with a brace inside a label value must parse")
    }
    if false == strings.Contains(labelBlock, `kind="x"`) {
        t.Fatalf("label block parsed as %q, wanted it to reach the last label", labelBlock)
    }
    if "3" != value {
        t.Fatalf("value parsed as %q, wanted 3", value)
    }
}

func TestSplitPrometheusLine_HandlesAnUnlabelledSample(t *testing.T) {
    name, labelBlock, value, parsed := splitPrometheusLine("go_goroutines 21")

    if false == parsed || "go_goroutines" != name || "" != labelBlock || "21" != value {
        t.Fatalf("an unlabelled sample parsed as name=%q labels=%q value=%q parsed=%v", name, labelBlock, value, parsed)
    }
}
