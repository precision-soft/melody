//go:build !melody_env_embedded && !melody_static_embedded

package testhelper

import (
    "testing"
)

func TestEmbeddedFilesystems_AnswerWhatThisBuildCarries(t *testing.T) {
    if nil != NewEmbeddedEnvironmentFs() {
        t.Fatalf("expected no embedded environment filesystem under this build")
    }

    if nil != NewEmbeddedStaticFs() {
        t.Fatalf("expected no embedded static filesystem under this build")
    }
}
