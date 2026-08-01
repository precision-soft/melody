package static

import (
    "testing"
)

/* @info the cache-control value is what tells a shared cache it may keep a copy of a static file and hand it to clients that never asked this application for it. The "no caching at all" branch had no test: a zero or negative age has to produce NO header rather than "public, max-age=0", because the first is silence and the second is an instruction a cache still acts on — it revalidates, which is not the same as never storing. */

func TestBuildCacheControlValue_APositiveAgeIsPubliclyCacheable(t *testing.T) {
    if "public, max-age=3600" != buildCacheControlValue(3600) {
        t.Fatalf("unexpected value: %q", buildCacheControlValue(3600))
    }

    if "public, max-age=1" != buildCacheControlValue(1) {
        t.Fatalf("unexpected value: %q", buildCacheControlValue(1))
    }
}

func TestBuildCacheControlValue_ANonPositiveAgeProducesNoValueAtAll(t *testing.T) {
    if "" != buildCacheControlValue(0) {
        t.Fatalf("expected a zero age to produce no header, got: %q", buildCacheControlValue(0))
    }

    if "" != buildCacheControlValue(-1) {
        t.Fatalf("expected a negative age to produce no header, got: %q", buildCacheControlValue(-1))
    }
}
