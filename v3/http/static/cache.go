package static

/* buildCacheControlValue honours zero as max-age=0 — always revalidate — the same reading the constructor gives an explicit zero; only a negative value, which the constructor never lets through, answers no header at all. */
func buildCacheControlValue(maxAge int) string {
    if 0 > maxAge {
        return ""
    }

    return "public, max-age=" + formatContentLength(int64(maxAge))
}
