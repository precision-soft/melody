package static

/* buildCacheControlValue answers zero as max-age=0; a negative value, which the constructor never lets through, answers no header. */
func buildCacheControlValue(maxAge int) string {
    if 0 > maxAge {
        return ""
    }

    return "public, max-age=" + formatContentLength(int64(maxAge))
}
