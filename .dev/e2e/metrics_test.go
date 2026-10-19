package main

import (
    "strings"
    "testing"
)

/* metricsTestExposition is a trimmed but faithful excerpt of what the example's /metrics answers: the otel scope labels are present on every series (a parser that demanded an exact label set would match nothing), the counter carries the exporter's _total suffix, and the histogram exposes _bucket, _sum and _count under one name. */
const metricsTestExposition = `# HELP http_server_request_count_total number of handled http requests
# TYPE http_server_request_count_total counter
http_server_request_count_total{http_request_method="GET",http_response_status_code="200",http_route="/health",otel_scope_name="melody.example",otel_scope_schema_url="",otel_scope_version=""} 12
http_server_request_count_total{http_request_method="GET",http_response_status_code="200",http_route="/i18n/greeting",otel_scope_name="melody.example",otel_scope_schema_url="",otel_scope_version=""} 0
http_server_request_count_total{http_request_method="POST",http_response_status_code="401",http_route="/health",otel_scope_name="melody.example",otel_scope_schema_url="",otel_scope_version=""} 5
# HELP http_server_request_duration_milliseconds duration of handled http requests in milliseconds
# TYPE http_server_request_duration_milliseconds histogram
http_server_request_duration_milliseconds_bucket{http_request_method="GET",http_response_status_code="200",http_route="/health",otel_scope_name="melody.example",le="5"} 9
http_server_request_duration_milliseconds_sum{http_request_method="GET",http_response_status_code="200",http_route="/health",otel_scope_name="melody.example"} 41.5
http_server_request_duration_milliseconds_count{http_request_method="GET",http_response_status_code="200",http_route="/health",otel_scope_name="melody.example"} 12
target_info{service_name="unknown"} 1
`

func TestPrometheusSeriesSum_ReadsTheRequestedSeries(t *testing.T) {
    value, found := prometheusSeriesSum(metricsTestExposition, metricsCounterPrefix, metricsCounterSuffix, map[string]string{
        "http_route":                "/health",
        "http_request_method":       "GET",
        "http_response_status_code": "200",
    })

    if false == found {
        t.Fatal("the /health GET 200 counter series must be found")
    }
    if 12 != value {
        t.Fatalf("the counter read as %g, wanted 12", value)
    }
}

/* the whole control assertion rests on this: a series that is ABSENT and a series that is present at zero are different findings, and a parser that reported both as 0 would let "the control route stayed unchanged" pass on a scrape where the metric had vanished entirely. */
func TestPrometheusSeriesSum_DistinguishesMissingFromZero(t *testing.T) {
    zero, zeroFound := prometheusSeriesSum(metricsTestExposition, metricsCounterPrefix, metricsCounterSuffix, map[string]string{
        "http_route":                "/i18n/greeting",
        "http_request_method":       "GET",
        "http_response_status_code": "200",
    })
    if false == zeroFound {
        t.Fatal("a series present with the value 0 must be reported as FOUND")
    }
    if 0 != zero {
        t.Fatalf("the zero series read as %g", zero)
    }

    missing, missingFound := prometheusSeriesSum(metricsTestExposition, metricsCounterPrefix, metricsCounterSuffix, map[string]string{
        "http_route":                "/never-called",
        "http_request_method":       "GET",
        "http_response_status_code": "200",
    })
    if true == missingFound {
        t.Fatal("a series that does not appear at all must be reported as NOT found")
    }
    if 0 != missing {
        t.Fatalf("a missing series must read as 0, got %g", missing)
    }
}

/* the labels select the series, so a different method or status on the same route is a different series: without this the /health counter would pick up the POST 401 line too and the exact-delta assertion would drift. */
func TestPrometheusSeriesSum_LabelsSelectTheSeries(t *testing.T) {
    value, found := prometheusSeriesSum(metricsTestExposition, metricsCounterPrefix, metricsCounterSuffix, map[string]string{
        "http_route":                "/health",
        "http_request_method":       "POST",
        "http_response_status_code": "401",
    })

    if false == found || 5 != value {
        t.Fatalf("the POST 401 series read as %g (found=%v), wanted 5", value, found)
    }
}

/* the histogram exposes three names under one prefix; without the suffix the sum would fold _bucket, _sum and _count together and report a number that means nothing. */
func TestPrometheusSeriesSum_SuffixSeparatesTheHistogramFamilies(t *testing.T) {
    count, countFound := prometheusSeriesSum(metricsTestExposition, metricsHistogramPrefix, metricsHistogramCountSuffix, map[string]string{
        "http_route": "/health",
    })
    if false == countFound || 12 != count {
        t.Fatalf("the histogram _count read as %g (found=%v), wanted 12", count, countFound)
    }

    sum, sumFound := prometheusSeriesSum(metricsTestExposition, metricsHistogramPrefix, "_sum", map[string]string{
        "http_route": "/health",
    })
    if false == sumFound || 41.5 != sum {
        t.Fatalf("the histogram _sum read as %g (found=%v), wanted 41.5", sum, sumFound)
    }
}

/* comment lines carry the metric name too, so a parser that did not skip them would read "# HELP" as a sample and report a bogus value or, worse, count a series that has no samples at all as found. */
func TestPrometheusSeriesSum_SkipsHelpAndTypeLines(t *testing.T) {
    body := "# HELP http_server_request_count_total number of handled http requests\n# TYPE http_server_request_count_total counter\n"

    if _, found := prometheusSeriesSum(body, metricsCounterPrefix, metricsCounterSuffix, map[string]string{}); true == found {
        t.Fatal("a body holding only # HELP / # TYPE lines carries no series")
    }
}

func TestSplitPrometheusLine_ReadsNameLabelsAndValue(t *testing.T) {
    name, labelBlock, value, parsed := splitPrometheusLine(`http_server_request_count_total{http_route="/health"} 12`)

    if false == parsed {
        t.Fatal("a well-formed sample line must parse")
    }
    if "http_server_request_count_total" != name {
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
