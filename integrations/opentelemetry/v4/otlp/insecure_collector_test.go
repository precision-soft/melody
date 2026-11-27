package otlp

import (
    "testing"
)

func TestCollectorIsLocal_AdmitsLocalhostAndLoopbackOnly(t *testing.T) {
    for endpoint, expected := range map[string]bool{
        "localhost:4317":      true,
        "LOCALHOST":           true,
        "127.0.0.1:4317":      true,
        "[::1]:4317":          true,
        "otel-collector:4317": false,
        "10.0.0.5:4317":       false,
        "collector.example":   false,
    } {
        if expected != collectorIsLocal(endpoint) {
            t.Fatalf("collectorIsLocal(%q): expected %v", endpoint, expected)
        }
    }
}

func TestInsecureCollectorWarningOf_NamesAnInsecureExportOffThisMachineOnly(t *testing.T) {
    for _, testCase := range []struct {
        endpoint string
        insecure bool
        warns    bool
    }{
        {"otel-collector:4317", true, true},
        {"otel-collector:4317", false, false},
        {"127.0.0.1:4317", true, false},
        {"", true, false},
    } {
        warning, warns := insecureCollectorWarningOf(testCase.endpoint, Config{Insecure: testCase.insecure})
        if testCase.warns != warns {
            t.Fatalf("%q insecure=%v: expected warns=%v, got %v", testCase.endpoint, testCase.insecure, testCase.warns, warns)
        }

        if true == warns && (InsecureCollectorBootWarningName != warning.Name || insecureCollectorWarningMessage != warning.Message || testCase.endpoint != warning.Context["endpoint"] || 1 != len(warning.Context)) {
            t.Fatalf("expected the warning naming the endpoint alone, got %v", warning)
        }
    }
}
