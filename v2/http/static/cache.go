package static

func buildCacheControlValue(maxAge int) string {
	if 0 >= maxAge {
		return ""
	}

	return "public, max-age=" + formatContentLength(int64(maxAge))
}
