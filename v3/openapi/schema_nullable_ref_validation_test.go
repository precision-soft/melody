package openapi

import (
    "reflect"
    "testing"
)

type emailRefTarget struct {
    Value string `json:"value"`
}

type emailRefParent struct {
    Child *emailRefTarget `json:"child" validate:"email"`
}

func TestApplyValidation_EmailFormatNotLeakedOntoNullableRefWrapper(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    buildSchema(reflect.TypeOf(emailRefParent{}), components, names, visited)

    parent := components["emailRefParent"]
    if nil == parent {
        t.Fatal("expected the parent struct to be registered in components")
    }

    child := parent.Properties["child"]
    if nil == child {
        t.Fatal("expected a 'child' property schema")
    }

    if 0 == len(child.AllOf) {
        t.Fatalf("expected the nullable struct reference to be wrapped in allOf; got %+v", child)
    }

    if "email" == child.Format {
        t.Fatal("the email format leaked onto the allOf-wrapped $ref; a validation keyword must not be a sibling of $ref/allOf")
    }
}
