package main

import (
    "testing"
)

func TestExampleAllowNamesReadsEveryListedMethod(t *testing.T) {
    if false == exampleAllowNames("GET, HEAD, OPTIONS", "GET", "HEAD", "OPTIONS") {
        t.Fatalf("expected every listed method to be found")
    }

    if false == exampleAllowNames("GET,HEAD", "HEAD") {
        t.Fatalf("expected a method listed without a space to be found")
    }
}

func TestExampleAllowNamesRefusesAMissingMethod(t *testing.T) {
    if true == exampleAllowNames("GET, HEAD", "GET", "OPTIONS") {
        t.Fatalf("expected a method the header does not list to be reported missing")
    }

    if true == exampleAllowNames("", "GET") {
        t.Fatalf("expected an empty header to name nothing")
    }

    if true == exampleAllowNames("GETX", "GET") {
        t.Fatalf("expected a method to be matched whole, not as a prefix")
    }
}
