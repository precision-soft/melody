package cli

import (
    "testing"
)

/* The full command path is proven live: the per-major band runs app:info against the built binary and greps these exact spellings. What is pinned here is the spelling itself, because the band's grep is only as strong as the line it looks for. */
func TestAppInfoSummaryLinesKeepTheGreppedSpellings(t *testing.T) {
    lines := appInfoSummaryLines(appInfoPayload{
        Env:             "dev",
        HttpAddress:     ":18081",
        PublicDir:       "public",
        StaticIndexFile: "index.html",
        Routes:          12,
        Services:        34,
    })

    expectedLines := []string{
        "env: dev",
        "http_address: :18081",
        "public_dir: public",
        "static_index_file: index.html",
        "routes: 12",
        "services: 34",
    }

    if len(expectedLines) != len(lines) {
        t.Fatalf("expected %d summary lines, got %v", len(expectedLines), lines)
    }
    for index, line := range lines {
        if expectedLines[index] != line {
            t.Fatalf("expected line %d to be %q, got %q", index, expectedLines[index], line)
        }
    }
}
