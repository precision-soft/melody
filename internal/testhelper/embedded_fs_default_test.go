//go:build !melody_env_embedded && !melody_static_embedded

package testhelper

import (
    "testing"
)

/* @info the four embedded-filesystem variants are selected by build tag, and which one a binary carries decides whether the application reads its environment and its static assets from disk or from the binary itself. Only one of them compiles per tag combination, so each carries its own mirror under the same tag — a variant whose accessor drifted would otherwise be discovered only by the combination of the four-tag matrix that happens to build it. */
func TestEmbeddedFilesystems_AnswerWhatThisBuildCarries(t *testing.T) {
    if nil != NewEmbeddedEnvFs() {
        t.Fatalf("expected no embedded environment filesystem under this build")
    }

    if nil != NewEmbeddedStaticFs() {
        t.Fatalf("expected no embedded static filesystem under this build")
    }
}
