package openapi

import (
    "reflect"
    "testing"
)

func TestBuildSchema_ByteSliceIsBase64String(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    /** encoding/json serializes a []byte slice as a base64 string, so the generated schema must be a
        {string, byte}, not an array of integers, or a client generated from the spec would send an integer
        array that BindJson rejects. */
    byteSlice := buildSchema(reflect.TypeOf([]byte{}), components, names, visited)
    if "string" != byteSlice.Type || "byte" != byteSlice.Format {
        t.Fatalf("expected []byte to be {string, byte}, got {%q, %q}", byteSlice.Type, byteSlice.Format)
    }

    stringSlice := buildSchema(reflect.TypeOf([]string{}), components, names, visited)
    if "array" != stringSlice.Type {
        t.Fatalf("expected []string to stay an array, got %q", stringSlice.Type)
    }

    /** Fixed byte arrays are not base64-encoded by encoding/json, so they stay integer arrays. */
    byteArray := buildSchema(reflect.TypeOf([4]byte{}), components, names, visited)
    if "array" != byteArray.Type {
        t.Fatalf("expected [4]byte to stay an array, got %q", byteArray.Type)
    }
}
