package report

import (
    "encoding/csv"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/repository"
    melodyhttp "github.com/precision-soft/melody/v3/http"
)

func TestCatalogReadingsCsv_WritesTheHeaderAndOneRowPerReadingInOrder(t *testing.T) {
    takenAt := time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)

    document, renderErr := catalogReadingsCsv([]*repository.CatalogReadingRecord{
        {TakenAt: takenAt, Headline: "12 products, 3 journal entries", Payload: `{"products":12}`, ProductCount: 12, JournalCount: 3},
        nil,
        {TakenAt: takenAt.Add(-time.Hour), Headline: "a headline, with a comma", Payload: "{}", ProductCount: 11, JournalCount: 0},
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    rows, parseErr := csv.NewReader(strings.NewReader(string(document))).ReadAll()
    if nil != parseErr {
        t.Fatalf("the document is not csv: %v", parseErr)
    }

    if 3 != len(rows) {
        t.Fatalf("expected the header and two rows, got %d: %q", len(rows), rows)
    }

    if "taken_at,headline,product_count,journal_count,payload" != strings.Join(rows[0], ",") {
        t.Fatalf("unexpected header %q", rows[0])
    }

    if "2026-09-29T12:00:00Z" != rows[1][0] || "12 products, 3 journal entries" != rows[1][1] || "12" != rows[1][2] || "3" != rows[1][3] || `{"products":12}` != rows[1][4] {
        t.Fatalf("unexpected first row %q", rows[1])
    }

    if "a headline, with a comma" != rows[2][1] {
        t.Fatalf("expected the comma kept inside its quoted cell, got %q", rows[2][1])
    }
}

func TestSpreadsheetSafeCell_KeepsAFormulaFromBeingEvaluated(t *testing.T) {
    for _, value := range []string{"=HYPERLINK(\"x\")", "+1", "-2", "@SUM(A1)", "\tlead", "\rlead"} {
        if "'"+value != spreadsheetSafeCell(value) {
            t.Fatalf("expected %q prefixed with an apostrophe, got %q", value, spreadsheetSafeCell(value))
        }
    }

    for _, value := range []string{"", "12 products", "{}", "a=b"} {
        if value != spreadsheetSafeCell(value) {
            t.Fatalf("expected %q unchanged, got %q", value, spreadsheetSafeCell(value))
        }
    }
}

func TestCatalogReadingsFileName_NamesTheExportDayInUtc(t *testing.T) {
    exportedAt := time.Date(2026, time.September, 30, 1, 30, 0, 0, time.FixedZone("Europe/Bucharest", 3*60*60))

    if "catalog-readings-2026-09-29.csv" != catalogReadingsFileName(exportedAt) {
        t.Fatalf("unexpected file name %q", catalogReadingsFileName(exportedAt))
    }

    if `attachment; filename="catalog-readings-2026-09-29.csv"` != melodyhttp.BuildContentDisposition("attachment", catalogReadingsFileName(exportedAt)) {
        t.Fatalf("unexpected disposition %q", melodyhttp.BuildContentDisposition("attachment", catalogReadingsFileName(exportedAt)))
    }
}
