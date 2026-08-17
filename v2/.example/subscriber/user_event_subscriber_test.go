package subscriber

import (
    "testing"

    "github.com/precision-soft/melody/v2/.example/service"
)

/* the helper is what lets one listener drop two spellings of one user — the previous and the current username of a rename — without either of them ever addressing the raw spelling the cache never held */
func TestUserUsernameCacheKeyFoldsTheSpelling(t *testing.T) {
    if expected := service.CacheKeyUserByUsername("café"); expected != userUsernameCacheKey(" Café ") {
        t.Fatalf("expected the folded key %q, got %q", expected, userUsernameCacheKey(" Café "))
    }
}

func TestUserUsernameCacheKeyAnswersEmptyForABlankUsername(t *testing.T) {
    if "" != userUsernameCacheKey("   ") {
        t.Fatalf("expected a blank username to answer no key, got %q", userUsernameCacheKey("   "))
    }
}
