package cache

import (
    "reflect"
    "strings"
    "time"
)

type layoutProbeBefore struct {
    Id   string
    Code string
}

type layoutProbeAfter struct {
    Id   string
    Code string
    Rate float64
}

type layoutProbeHolder struct {
    Nested   layoutProbeBefore
    Stamped  time.Time
    Pointers []*layoutProbeAfter
    hidden   int
}

type reflectType = reflect.Type

func reflectTypeOf(value any) reflect.Type {
    return reflect.TypeOf(value)
}

func contains(haystack string, needle string) bool {
    return strings.Contains(haystack, needle)
}
