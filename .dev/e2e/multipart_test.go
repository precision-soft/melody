package main

import (
    "bytes"
    "mime"
    "mime/multipart"
    "strings"
    "testing"
)

/* The envelope builder decides what the multipart assertion is actually asserting: the section compares the byte count the handler reported against len(envelope), so an envelope whose Content-Type boundary did not match its body would be rejected by the framework as malformed and the section would report a framework defect that does not exist. */
func TestBuildMultipartEnvelope_BoundaryMatchesTheBody(t *testing.T) {
    envelope, contentType := buildMultipartEnvelope(
        map[string]string{"label": "a field"},
        "attachment",
        "payload.bin",
        []byte("file bytes"),
    )

    mediaType, parameters, parseErr := mime.ParseMediaType(contentType)
    if nil != parseErr {
        t.Fatalf("the returned content type does not parse: %v", parseErr)
    }
    if "multipart/form-data" != mediaType {
        t.Fatalf("media type is %q, wanted multipart/form-data", mediaType)
    }

    boundary := parameters["boundary"]
    if "" == boundary {
        t.Fatal("the content type carries no boundary, so no server can parse the body")
    }
    if false == bytes.Contains(envelope, []byte("--"+boundary)) {
        t.Fatal("the body does not carry the boundary the content type declares")
    }
}

/* the envelope has to be parseable by a real multipart reader: the property under test is that the FRAMEWORK left the body unread, which says nothing at all if the body was never a valid envelope in the first place. */
func TestBuildMultipartEnvelope_ParsesBackIntoItsParts(t *testing.T) {
    fileContent := []byte("the file part's own bytes")

    envelope, contentType := buildMultipartEnvelope(
        map[string]string{"label": "a field", "note": "another"},
        "attachment",
        "payload.bin",
        fileContent,
    )

    _, parameters, parseErr := mime.ParseMediaType(contentType)
    if nil != parseErr {
        t.Fatalf("parse the content type: %v", parseErr)
    }

    reader := multipart.NewReader(bytes.NewReader(envelope), parameters["boundary"])

    form, readErr := reader.ReadForm(1 << 20)
    if nil != readErr {
        t.Fatalf("the envelope does not parse as a form: %v", readErr)
    }

    if "a field" != form.Value["label"][0] {
        t.Fatalf("the label field parsed as %q", form.Value["label"][0])
    }
    if "another" != form.Value["note"][0] {
        t.Fatalf("the note field parsed as %q", form.Value["note"][0])
    }

    fileHeaderList, hasFile := form.File["attachment"]
    if false == hasFile || 0 == len(fileHeaderList) {
        t.Fatal("the envelope carries no file part")
    }
    if "payload.bin" != fileHeaderList[0].Filename {
        t.Fatalf("the file part is named %q, wanted payload.bin", fileHeaderList[0].Filename)
    }
    if int64(len(fileContent)) != fileHeaderList[0].Size {
        t.Fatalf("the file part holds %d bytes, wanted %d", fileHeaderList[0].Size, len(fileContent))
    }
}

/* the missing-key probe posts a fields-only envelope, so an empty filename must produce a valid envelope with no file part rather than an empty part the framework might reject for its own reasons. */
func TestBuildMultipartEnvelope_OmitsTheFilePartWithoutAFilename(t *testing.T) {
    envelope, contentType := buildMultipartEnvelope(map[string]string{"label": "no file here"}, "", "", nil)

    if true == strings.Contains(string(envelope), "filename=") {
        t.Fatal("an envelope built without a filename must carry no file part")
    }
    if false == strings.Contains(string(envelope), "no file here") {
        t.Fatal("the fields-only envelope lost its field")
    }
    if false == strings.HasPrefix(contentType, "multipart/form-data") {
        t.Fatalf("content type is %q", contentType)
    }
}
