package wiring

import (
    "testing"
)

/* @info a build context carries plain tag identifiers; a constraint expression handed to it matches no file, so the scan would behave as if nothing had been passed and strict would still report success */
func TestSplitBuildTags_RejectsAConstraintExpression(t *testing.T) {
    for _, tags := range []string{"!postgres", "postgres,!mysql", "postgres mysql", "post-gres", "(postgres)"} {
        buildTags, splitErr := splitBuildTags(tags)

        if nil == splitErr {
            t.Fatalf("expected %q to be rejected, got tags %v", tags, buildTags)
        }

        if nil != buildTags {
            t.Fatalf("expected %q to yield no tags, got %v", tags, buildTags)
        }
    }
}

/* @info the accepted forms follow the go tool's own tag syntax, and surrounding spaces and empty entries stay tolerated */
func TestSplitBuildTags_AcceptsPlainIdentifiers(t *testing.T) {
    buildTags, splitErr := splitBuildTags(" with_postgres , go1.22,, Integration2 ")
    if nil != splitErr {
        t.Fatalf("expected plain identifiers to be accepted: %v", splitErr)
    }

    expected := []string{"with_postgres", "go1.22", "Integration2"}
    if len(expected) != len(buildTags) {
        t.Fatalf("expected %v, got %v", expected, buildTags)
    }

    for index, tag := range expected {
        if tag != buildTags[index] {
            t.Fatalf("expected %v, got %v", expected, buildTags)
        }
    }
}

func TestSplitBuildTags_EmptyInputYieldsNoTags(t *testing.T) {
    buildTags, splitErr := splitBuildTags("")
    if nil != splitErr {
        t.Fatalf("expected an empty tag list to be accepted: %v", splitErr)
    }

    if nil != buildTags {
        t.Fatalf("expected an empty tag list to yield no tags, got %v", buildTags)
    }
}
