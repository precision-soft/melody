package translation

import (
    "testing"
)

func TestRomanianPluralCategory(t *testing.T) {
    cases := []struct {
        number   float64
        expected string
    }{
        {0, "few"},
        {1, "one"},
        {2, "few"},
        {19, "few"},
        {20, "other"},
        {21, "other"},
        {100, "other"},
        {101, "few"},
        {119, "few"},
        {120, "other"},
        {201, "few"},
        {1001, "few"},
        {1.5, "few"},
    }

    for _, testCase := range cases {
        result := pluralCategory("ro", testCase.number)
        if testCase.expected != result {
            t.Fatalf("pluralCategory(ro, %v) = %q, want %q", testCase.number, result, testCase.expected)
        }
    }
}
