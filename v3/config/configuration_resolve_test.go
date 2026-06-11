package config

import (
    "testing"
)

func TestEnvPlaceholderPattern_RejectsIdentifiersStartingWithDigit(t *testing.T) {
    if true == envPlaceholderPattern.MatchString("%env(1INVALID)%") {
        t.Fatalf("expected pattern to reject identifier starting with digit")
    }
}

func TestEnvPlaceholderPattern_AcceptsIdentifiersStartingWithLetterOrUnderscore(t *testing.T) {
    if false == envPlaceholderPattern.MatchString("%env(VALID_KEY)%") {
        t.Fatalf("expected pattern to accept identifier starting with letter")
    }

    if false == envPlaceholderPattern.MatchString("%env(_VALID)%") {
        t.Fatalf("expected pattern to accept identifier starting with underscore")
    }
}

func TestParameterPlaceholderPattern_RejectsIdentifiersStartingWithDigit(t *testing.T) {
    if true == parameterPlaceholderPattern.MatchString("%1invalid%") {
        t.Fatalf("expected pattern to reject identifier starting with digit")
    }
}

func TestParameterPlaceholderPattern_AcceptsDottedIdentifiers(t *testing.T) {
    if false == parameterPlaceholderPattern.MatchString("%kernel.project_dir%") {
        t.Fatalf("expected pattern to accept dotted identifier")
    }
}
