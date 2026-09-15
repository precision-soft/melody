package cli

import (
    "bytes"
    "errors"
    "strings"
    "testing"
)

func TestProductListCommandUsesRunnerWriter(t *testing.T) {
    buffer := &bytes.Buffer{}
    runErr := NewProductListCommand().Run(newProductListRuntime(t, nil, nil), &flagContext{writer: buffer})
    if nil != runErr || false == strings.Contains(buffer.String(), "product list: limit=0") || false == strings.Contains(buffer.String(), "CATEGORY") {
        t.Fatalf("output=%q error=%v", buffer.String(), runErr)
    }
}

func TestProductListCommandReturnsLookupFailures(t *testing.T) {
    failure := errors.New("lookup refused")
    for _, kind := range []string{"category", "currency"} {
        t.Run(kind, func(t *testing.T) {
            var categoryFailure, currencyFailure error
            if "category" == kind {
                categoryFailure = failure
            } else {
                currencyFailure = failure
            }
            runErr := NewProductListCommand().Run(newProductListRuntime(t, categoryFailure, currencyFailure), &flagContext{})
            if false == errors.Is(runErr, failure) {
                t.Fatalf("lookup failure lost: %v", runErr)
            }
        })
    }
}

func TestProductListCommandReturnsWriterFailure(t *testing.T) {
    failure := errors.New("writer closed")
    runErr := NewProductListCommand().Run(newProductListRuntime(t, nil, nil), &flagContext{writer: &failedCommandWriter{failure: failure}})
    if false == errors.Is(runErr, failure) {
        t.Fatalf("writer failure lost: %v", runErr)
    }
}
