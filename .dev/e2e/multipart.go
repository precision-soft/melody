package main

import (
    "bytes"
    "mime/multipart"
    "net/http"
    "net/url"
)

const multipartLabel = "multipart body"

/* the storage demo is the only route in the example that reports the byte count its handler saw, which is what
makes it the right probe: PutHandler reads the RAW request body and answers with its length
(handler/storage/storage_handler.go), so the number in the response is a direct measurement of what the framework
left for the handler to read. */
const multipartRoute = "/storage/object"

/* multipartPutPayload and multipartGetPayload mirror the two handlers' response payloads. */
type multipartPutPayload struct {
    Stored string `json:"stored"`
    Bytes  int    `json:"bytes"`
}

type multipartGetPayload struct {
    Key     string `json:"key"`
    Content string `json:"content"`
}

/* runMultipartCheck asserts what v3 actually does with a form body, which is not what a reader expects and is
therefore worth pinning:

  - a MULTIPART body is left UNREAD. NewRequest auto-parses form content types, but it deliberately does not buffer
    a multipart body — ParseForm does not consume one, and a handler streams it through ParseMultipartForm with the
    large-upload disk spooling intact (v3/http/request.go). So the property under test is that the handler still
    finds the WHOLE envelope, byte for byte: a regression that started buffering or draining it would show up as a
    byte count below what was sent, or as a truncated object in the store;
  - a URLENCODED body IS drained by ParseForm and then RESTORED, twice, around the parse. That restore exists
    because the HMAC internal-auth source verifies a hash of the raw body: without it the hash is computed over an
    empty body and a tampered form-encoded request is silently accepted. A regression there yields a byte count of
    ZERO, which is exactly what the urlencoded half asserts against.

This section does NOT re-assert the declared-size contract (a Put whose declared size is shorter than its body must
be refused before the bucket is touched, and the object already at the key must survive). That property lives in
objectstorage.go, which drives the awss3 Put directly and can therefore lie about the size — something no http
client can do, since the framework derives the length from the body it actually read. The two must never grow into
the same coverage: this one is about what the FRAMEWORK leaves in the body, that one is about what the STORAGE layer
does with a size it was handed. */
func runMultipartCheck(baseUrl string) {
    client := newLiveExampleClient(baseUrl)

    assertMultipartBodyReachesHandlerIntact(client)
    assertUrlEncodedBodyIsRestoredAfterParsing(client)
    assertMultipartMissingKeyRefused(client)
    assertMultipartUnknownKeyNotFound(client)
}

func assertMultipartBodyReachesHandlerIntact(client *liveExampleClient) {
    key := liveExampleUnique("e2e-multipart")

    envelope, contentType := buildMultipartEnvelope(map[string]string{
        "label": "a plain form field",
        "note":  "another one, so the envelope carries more than a single part",
    }, "attachment", "payload.bin", []byte("the file part's own bytes, which must survive the trip untouched"))

    response := client.call(multipartLabel, liveExampleRequest{
        method:      "POST",
        path:        multipartRoute + "?key=" + url.QueryEscape(key),
        contentType: contentType,
        body:        envelope,
    })
    requireLiveExampleStatus(multipartLabel, "the multipart put", response, http.StatusOK)

    put := multipartPutPayload{}
    decodeLiveExamplePayload(multipartLabel, response, &put)

    if len(envelope) != put.Bytes {
        fail(
            "%s: the handler saw %d bytes of a %d-byte multipart envelope — the framework consumed part of the body before the handler read it, so a handler that streams its own multipart parse now receives a truncated stream",
            multipartLabel,
            put.Bytes,
            len(envelope),
        )
    }
    if key != put.Stored {
        fail("%s: the put reports key %q, wanted %q", multipartLabel, put.Stored, key)
    }
    pass("the handler read all %d bytes of the multipart envelope (the framework left the body unread)", put.Bytes)

    /* the byte COUNT alone would also be satisfied by a body whose bytes were reordered or re-encoded, so the
       stored object is read back and compared byte for byte */
    stored := readMultipartStoredObject(client, key)
    if false == bytes.Equal(envelope, stored) {
        fail(
            "%s: the object stored under %q differs from the envelope that was posted (%d bytes stored, %d sent)",
            multipartLabel,
            key,
            len(stored),
            len(envelope),
        )
    }
    pass("the stored object is byte-identical to the multipart envelope that was posted")
}

/* assertUrlEncodedBodyIsRestoredAfterParsing is the twin half: the same route, the same measurement, a body the
framework DOES drain. A zero byte count here is the drain-without-restore regression. */
func assertUrlEncodedBodyIsRestoredAfterParsing(client *liveExampleClient) {
    key := liveExampleUnique("e2e-urlencoded")

    form := url.Values{}
    form.Set("label", "a urlencoded field")
    form.Set("note", "which ParseForm consumes and the framework has to put back")
    body := []byte(form.Encode())

    response := client.call(multipartLabel, liveExampleRequest{
        method:      "POST",
        path:        multipartRoute + "?key=" + url.QueryEscape(key),
        contentType: "application/x-www-form-urlencoded",
        body:        body,
    })
    requireLiveExampleStatus(multipartLabel, "the urlencoded put", response, http.StatusOK)

    put := multipartPutPayload{}
    decodeLiveExamplePayload(multipartLabel, response, &put)

    if 0 == put.Bytes {
        fail(
            "%s: the handler saw ZERO bytes of a %d-byte urlencoded body — the form parse drained it and never restored it, so every later reader (the internal-auth body hash above all) verifies against an empty body",
            multipartLabel,
            len(body),
        )
    }
    if len(body) != put.Bytes {
        fail(
            "%s: the handler saw %d bytes of a %d-byte urlencoded body — the body was restored only partially",
            multipartLabel,
            put.Bytes,
            len(body),
        )
    }

    stored := readMultipartStoredObject(client, key)
    if false == bytes.Equal(body, stored) {
        fail("%s: the stored urlencoded body differs from the one that was posted", multipartLabel)
    }

    pass("the urlencoded body was drained by the form parse and restored intact for the handler (%d bytes)", put.Bytes)
}

func assertMultipartMissingKeyRefused(client *liveExampleClient) {
    envelope, contentType := buildMultipartEnvelope(map[string]string{"label": "no key was given"}, "", "", nil)

    response := client.call(multipartLabel, liveExampleRequest{
        method:      "POST",
        path:        multipartRoute,
        contentType: contentType,
        body:        envelope,
    })

    if http.StatusBadRequest != response.statusCode {
        fail(
            "%s: a put with no key parameter answered %d, wanted 400 — a missing key must not be stored under an empty one: %s",
            multipartLabel,
            response.statusCode,
            exampleTruncate(response.bodyText()),
        )
    }

    pass("a put with no key parameter was refused with 400")
}

func assertMultipartUnknownKeyNotFound(client *liveExampleClient) {
    key := liveExampleUnique("e2e-never-written")

    response := client.get(multipartLabel, multipartRoute+"?key="+url.QueryEscape(key))
    if http.StatusNotFound != response.statusCode {
        fail(
            "%s: reading a key that was never written answered %d, wanted 404: %s",
            multipartLabel,
            response.statusCode,
            exampleTruncate(response.bodyText()),
        )
    }

    pass("reading a key that was never written answered 404")
}

func readMultipartStoredObject(client *liveExampleClient, key string) []byte {
    response := client.get(multipartLabel, multipartRoute+"?key="+url.QueryEscape(key))
    requireLiveExampleStatus(multipartLabel, "reading back "+key, response, http.StatusOK)

    payload := multipartGetPayload{}
    decodeLiveExamplePayload(multipartLabel, response, &payload)

    if key != payload.Key {
        fail("%s: the read reports key %q, wanted %q", multipartLabel, payload.Key, key)
    }

    return []byte(payload.Content)
}

/* buildMultipartEnvelope assembles a REAL mime/multipart envelope and returns it together with the Content-Type
carrying the boundary the writer chose. Hand-writing the envelope would risk a body the framework rejects as
malformed for a reason unrelated to the property under test — and the returned Content-Type has to come from the
writer, because a boundary that does not match the body makes every assertion below meaningless. A file part is
included only when a filename is given, so the missing-key probe can post a fields-only envelope. */
func buildMultipartEnvelope(fields map[string]string, fileField string, fileName string, fileContent []byte) ([]byte, string) {
    buffer := &bytes.Buffer{}
    writer := multipart.NewWriter(buffer)

    for name, value := range fields {
        if writeErr := writer.WriteField(name, value); nil != writeErr {
            fail("%s: write the multipart field %q: %v", multipartLabel, name, writeErr)
        }
    }

    if "" != fileName {
        part, partErr := writer.CreateFormFile(fileField, fileName)
        if nil != partErr {
            fail("%s: create the multipart file part: %v", multipartLabel, partErr)
        }

        if _, writeErr := part.Write(fileContent); nil != writeErr {
            fail("%s: write the multipart file part: %v", multipartLabel, writeErr)
        }
    }

    if closeErr := writer.Close(); nil != closeErr {
        fail("%s: close the multipart writer: %v", multipartLabel, closeErr)
    }

    return buffer.Bytes(), writer.FormDataContentType()
}
