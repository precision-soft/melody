//go:build ignore

package domain

/* a file the default build excludes must contribute nothing to the wiring: its constructor exists only under a tag the generated file is not built with */

func NewBuildExcluded() *BuildExcluded {
    return &BuildExcluded{}
}

type BuildExcluded struct {
}
