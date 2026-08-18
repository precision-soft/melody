package config

import "testing"

func TestDialIsInsecureOnlyOnTheExactSpelling(t *testing.T) {
    if false == dialIsInsecure("true") {
        t.Fatal("expected the exact spelling to arm the insecure dial")
    }

    for _, value := range []string{"", "false", "TRUE", "1", "yes", " true"} {
        if true == dialIsInsecure(value) {
            t.Fatalf("expected %q to keep the verified handshake", value)
        }
    }
}
