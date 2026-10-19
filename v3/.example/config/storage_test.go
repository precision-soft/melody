package config

import (
    "testing"
)

func TestObjectStorageIsSecure_DialsHttpsUnlessTheInsecureSwitchIsExactlyTrue(t *testing.T) {
    for insecureValue, secure := range map[string]bool{
        "":      true,
        "false": true,
        "TRUE":  true,
        "1":     true,
        "true":  false,
    } {
        if secure != objectStorageIsSecure(insecureValue) {
            t.Fatalf("expected S3_INSECURE=%q to dial secure=%v", insecureValue, secure)
        }
    }
}
