package rueidis

import (
    "strings"
)

func escapeRedisGlobMeta(value string) string {
    var builder strings.Builder

    for index := 0; index < len(value); index++ {
        switch value[index] {
        case '*', '?', '[', ']', '\\':
            builder.WriteByte('\\')
        }

        builder.WriteByte(value[index])
    }

    return builder.String()
}
