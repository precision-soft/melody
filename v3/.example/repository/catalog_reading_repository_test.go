package repository

import (
    "testing"
    "time"
)

func TestValidateCatalogReadingNamesTheFirstFieldItFailsOn(t *testing.T) {
    takenAt := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)

    testCases := map[string]struct {
        reading  *CatalogReadingRecord
        expected string
    }{
        "absent reading":         {reading: nil, expected: "reading is required"},
        "zero instant":           {reading: &CatalogReadingRecord{Headline: "catalogue"}, expected: "taken at is required"},
        "empty headline":         {reading: &CatalogReadingRecord{TakenAt: takenAt}, expected: "headline is required"},
        "negative product count": {reading: &CatalogReadingRecord{TakenAt: takenAt, Headline: "catalogue", ProductCount: -1}, expected: "counts may not be negative"},
        "negative journal count": {reading: &CatalogReadingRecord{TakenAt: takenAt, Headline: "catalogue", JournalCount: -1}, expected: "counts may not be negative"},
    }

    for name, testCase := range testCases {
        t.Run(name, func(t *testing.T) {
            validationErr := validateCatalogReading(testCase.reading)
            if nil == validationErr || testCase.expected != validationErr.Error() {
                t.Fatalf("expected %q, got %v", testCase.expected, validationErr)
            }
        })
    }

    if validationErr := validateCatalogReading(&CatalogReadingRecord{TakenAt: takenAt, Headline: "catalogue"}); nil != validationErr {
        t.Fatalf("expected a complete reading to pass, got %v", validationErr)
    }
}
