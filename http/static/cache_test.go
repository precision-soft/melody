package static

import (
    "testing"
)

func TestBuildCacheControlValue_APositiveAgeIsPubliclyCacheable(t *testing.T) {
    if "public, max-age=3600" != buildCacheControlValue(3600) {
        t.Fatalf("unexpected value: %q", buildCacheControlValue(3600))
    }

    if "public, max-age=1" != buildCacheControlValue(1) {
        t.Fatalf("unexpected value: %q", buildCacheControlValue(1))
    }
}

func TestBuildCacheControlValue_ZeroIsAlwaysRevalidateAndNegativeIsSilence(t *testing.T) {
    if "public, max-age=0" != buildCacheControlValue(0) {
        t.Fatalf("expected the explicit zero to instruct revalidation, got: %q", buildCacheControlValue(0))
    }

    if "" != buildCacheControlValue(-1) {
        t.Fatalf("expected a negative age to produce no header, got: %q", buildCacheControlValue(-1))
    }
}
