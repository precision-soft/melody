//go:build !melody_env_embedded && melody_static_embedded

package testhelper

import (
    "testing"
)

func TestEmbeddedFilesystems_AnswerWhatThisBuildCarries(t *testing.T) {
    if nil != NewEmbeddedEnvFs() {
        t.Fatalf("expected no embedded environment filesystem under this build")
    }

    if nil == NewEmbeddedStaticFs() {
        t.Fatalf("expected an embedded static filesystem under this build")
    }
}
