package internal

import "testing"

func TestEnvironmentKeyOwners_AreDistinct(t *testing.T) {
    if EnvironmentKeyOwnerFramework == EnvironmentKeyOwnerApplication {
        t.Fatalf("expected two owner names")
    }
}
