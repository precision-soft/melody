package static

import (
	"fmt"
	"io/fs"
)

func GenerateEtag(info fs.FileInfo, weak bool) string {
	if nil == info {
		return ""
	}

	etag := fmt.Sprintf("%d-%d", info.Size(), info.ModTime().Unix())

	if true == weak {
		return fmt.Sprintf("W/%q", etag)
	}

	return fmt.Sprintf("%q", etag)
}
