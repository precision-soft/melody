package http

import "strings"

func JoinPaths(prefix string, pattern string) string {
	if "" == prefix {
		return pattern
	}

	if "" == pattern {
		return prefix
	}

	cleanPrefix := prefix
	if false == strings.HasPrefix(cleanPrefix, "/") {
		cleanPrefix = "/" + cleanPrefix
	}
	cleanPrefix = strings.TrimSuffix(cleanPrefix, "/")

	cleanPattern := pattern
	if false == strings.HasPrefix(cleanPattern, "/") {
		cleanPattern = "/" + cleanPattern
	}

	return cleanPrefix + cleanPattern
}
