package audit

import (
    "testing"
)

func TestCloneModel_DecouplesPointerSliceAndMapFields(t *testing.T) {
    type entity struct {
        Name   *string
        Tags   []string
        Labels map[string]string
    }

    name := "before"
    model := &entity{
        Name:   &name,
        Tags:   []string{"x"},
        Labels: map[string]string{"k": "before"},
    }

    cloned, cloneErr := cloneModel(model)
    if nil != cloneErr {
        t.Fatalf("clone: %v", cloneErr)
    }

    typedClone := cloned.(*entity)
    *typedClone.Name = "after"
    typedClone.Tags[0] = "y"
    typedClone.Labels["k"] = "after"

    if "before" != *model.Name {
        t.Fatalf("clone aliased the original pointer field; bun would scan the old row in-place over the live model, got %q", *model.Name)
    }
    if "x" != model.Tags[0] {
        t.Fatalf("clone aliased the original slice field, got %q", model.Tags[0])
    }
    if "before" != model.Labels["k"] {
        t.Fatalf("clone aliased the original map field, got %q", model.Labels["k"])
    }
}
