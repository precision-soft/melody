package config

import (
    "testing"
)

func TestObjectStorageIsInsecure_DialsPlaintextOnlyWhenTheInsecureSwitchIsExactlyTrue(t *testing.T) {
    for insecureValue, insecure := range map[string]bool{
        "":      false,
        "false": false,
        "TRUE":  false,
        "1":     false,
        "true":  true,
    } {
        if insecure != objectStorageIsInsecure(insecureValue) {
            t.Fatalf("expected S3_INSECURE=%q to dial insecure=%v", insecureValue, insecure)
        }
    }
}
