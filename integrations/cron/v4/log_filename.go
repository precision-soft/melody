package cron

import (
    "strings"
)

func splitLogFileExtension(name string) (string, string) {
    start := 0
    for start < len(name) && '.' == name[start] {
        start++
    }

    if start >= len(name) {
        return name, ""
    }

    dotIndex := strings.IndexByte(name[start:], '.')
    if -1 == dotIndex {
        return name, ""
    }

    return name[:start+dotIndex], name[start+dotIndex:]
}

func sanitizeLogFileName(commandName string) string {
    name := strings.ReplaceAll(commandName, ":", "-")
    name = strings.ReplaceAll(name, "/", "-")

    return name
}

func rawLogFileName(commandName string) string {
    return strings.ReplaceAll(commandName, "/", "-")
}
