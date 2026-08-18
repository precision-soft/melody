package main

import (
    "encoding/json"
    "net/http"
    "sort"
    "strings"
)

const openApiLabel = "openapi served"

/* openApiRoute is the route the example publishes the document on (config/http.go), and it is PUBLIC there — the
access control lists ^/openapi.json — so an anonymous probe is the right probe. */
const openApiRoute = "/openapi.json"

/* openApiExpectedTitle mirrors the Info the example builds in config/openapi.go. Asserting it is what proves the
document came from THIS application's registry rather than from a generator default. */
const openApiExpectedTitle = "Melody Example API"

/* openApiDescribedOperationCount is how many operations config/openapi.go describes by hand (the product create
and the i18n greeting). The path count of the served document must exceed it: a document generated from an empty
router carries only the described operations, and everything below would then assert on a document that never saw
the booted route table. */
const openApiDescribedOperationCount = 2

/* the operation the section pins. It is chosen because it is the one the example describes with a TYPED response,
so it proves both halves of the generator at once: the operation exists because the ROUTER reported the route, and
its response body references a component schema because the REGISTRY described the handler's own type. A generator
that lost the registry would still emit the operation, with the bare "default" response every undescribed route
gets — which is exactly the regression the $ref assertion catches. */
const (
    openApiPinnedPath      = "/i18n/greeting"
    openApiPinnedMethod    = "get"
    openApiPinnedOperation = "example.i18n.greeting"
    openApiPinnedSchema    = "GreetingResponse"
)

/* runOpenApiCheck proves the document the SERVING process produces, which is a different property from stack.sh's
OPENAPI GENERATE check: that one greps a file a command wrote in a separate process, so it can only ever prove the
generator. This one asks the live application for its own document over http and therefore also covers the handler,
the router the handler reads its routes from and the registry it resolves out of the container. */
func runOpenApiCheck(baseUrl string) {
    client := newLiveExampleClient(baseUrl)

    response := client.get(openApiLabel, openApiRoute)
    requireLiveExampleStatus(openApiLabel, openApiRoute, response, http.StatusOK)

    /* the decode is itself a negative: an html error page, a proxy notice or a login redirect body served with a
       200 all fail here, and each of those would satisfy a status-only assertion */
    document := map[string]any{}
    if decodeErr := json.Unmarshal(response.body, &document); nil != decodeErr {
        fail(
            "%s: %s answered 200 with a body that is not json (%v): %s",
            openApiLabel,
            openApiRoute,
            decodeErr,
            exampleTruncate(response.bodyText()),
        )
    }
    pass("%s decoded as json over live http", openApiRoute)

    assertOpenApiInfo(document)
    assertOpenApiBootedRoutes(document)
    assertOpenApiReferencesResolve(document)
}

func assertOpenApiInfo(document map[string]any) {
    info, isMap := document["info"].(map[string]any)
    if false == isMap {
        fail("%s: the document carries no info object", openApiLabel)
    }

    title, isString := info["title"].(string)
    if false == isString {
        fail("%s: info.title is missing or not a string", openApiLabel)
    }
    if openApiExpectedTitle != title {
        fail(
            "%s: info.title is %q, wanted %q — the served document was not built from the example's own Info",
            openApiLabel,
            title,
            openApiExpectedTitle,
        )
    }
    pass("the served document carries the example's own info.title (%q)", title)
}

/* assertOpenApiBootedRoutes is the "generated from the real router" half. Three things are asserted and each one
fails on a different regression: the path count exceeds the number of hand-described operations (a document built
from an empty router would not), the pinned operation is present under its declared method with its declared
operation id (a router whose route names were lost would emit a synthesised one), and its 200 response references a
component schema (a handler that lost the registry emits the bare "default" response instead). */
func assertOpenApiBootedRoutes(document map[string]any) {
    paths, isMap := document["paths"].(map[string]any)
    if false == isMap {
        fail("%s: the document carries no paths object", openApiLabel)
    }

    if openApiDescribedOperationCount >= len(paths) {
        fail(
            "%s: the document holds %d path(s), which is at or below the %d operation(s) config/openapi.go describes by hand — it was generated from an empty router, so nothing here reflects the booted route table",
            openApiLabel,
            len(paths),
            openApiDescribedOperationCount,
        )
    }
    pass("the document holds %d paths, more than the %d hand-described operations (it saw the booted router)", len(paths), openApiDescribedOperationCount)

    pathItem, pathExists := paths[openApiPinnedPath].(map[string]any)
    if false == pathExists {
        fail(
            "%s: the document has no %q path — the booted route table holds it, so the generator dropped it (paths present: %s)",
            openApiLabel,
            openApiPinnedPath,
            strings.Join(openApiSortedKeys(paths), ", "),
        )
    }

    operation, operationExists := pathItem[openApiPinnedMethod].(map[string]any)
    if false == operationExists {
        fail("%s: %q carries no %q operation", openApiLabel, openApiPinnedPath, openApiPinnedMethod)
    }

    operationId, isString := operation["operationId"].(string)
    if false == isString || openApiPinnedOperation != operationId {
        fail(
            "%s: %s %s reports operationId %q, wanted the route name %q",
            openApiLabel,
            openApiPinnedMethod,
            openApiPinnedPath,
            operationId,
            openApiPinnedOperation,
        )
    }
    pass("%s %s carries the booted route's own name as its operationId (%s)", openApiPinnedMethod, openApiPinnedPath, operationId)

    reference := openApiResponseSchemaReference(operation)
    if "" == reference {
        fail(
            "%s: the 200 response of %s %s references no component schema — the handler produced a document without the registry's descriptors, so every described response degraded to the bare \"default\" one",
            openApiLabel,
            openApiPinnedMethod,
            openApiPinnedPath,
        )
    }
    if false == strings.HasSuffix(reference, "/"+openApiPinnedSchema) {
        fail(
            "%s: the 200 response of %s %s references %q, wanted the %s component",
            openApiLabel,
            openApiPinnedMethod,
            openApiPinnedPath,
            reference,
            openApiPinnedSchema,
        )
    }
    pass("the 200 response of %s %s references the %s component schema", openApiPinnedMethod, openApiPinnedPath, openApiPinnedSchema)
}

func openApiResponseSchemaReference(operation map[string]any) string {
    responses, isMap := operation["responses"].(map[string]any)
    if false == isMap {
        return ""
    }

    response, exists := responses["200"].(map[string]any)
    if false == exists {
        return ""
    }

    content, hasContent := response["content"].(map[string]any)
    if false == hasContent {
        return ""
    }

    mediaType, hasMediaType := content["application/json"].(map[string]any)
    if false == hasMediaType {
        return ""
    }

    schema, hasSchema := mediaType["schema"].(map[string]any)
    if false == hasSchema {
        return ""
    }

    reference, isString := schema["$ref"].(string)
    if false == isString {
        return ""
    }

    return reference
}

/* assertOpenApiReferencesResolve walks the whole document and resolves every $ref against it. An unresolvable one
is a document no tooling can read — a generator that emitted a reference to a component it forgot to register
produces a file that validates as json, passes a status assertion, and breaks every client generator that reads
it. The walk is a graph walk over maps and slices, so it also covers references nested inside array items and
inside composed schemas. */
func assertOpenApiReferencesResolve(document map[string]any) {
    referenceList := []string{}
    openApiCollectReferences(document, &referenceList)

    if 0 == len(referenceList) {
        fail(
            "%s: the document holds no $ref at all — the pinned operation was asserted to carry one, so the walk itself is broken",
            openApiLabel,
        )
    }

    for _, reference := range referenceList {
        if false == openApiResolves(document, reference) {
            fail(
                "%s: the document references %q, which resolves to nothing inside it — every client generator that reads this document breaks on it",
                openApiLabel,
                reference,
            )
        }
    }

    pass("all %d $ref(s) in the served document resolve inside it", len(referenceList))
}

func openApiCollectReferences(node any, into *[]string) {
    switch typed := node.(type) {
    case map[string]any:
        for key, value := range typed {
            if "$ref" == key {
                if reference, isString := value.(string); true == isString {
                    *into = append(*into, reference)
                }

                continue
            }

            openApiCollectReferences(value, into)
        }
    case []any:
        for _, entry := range typed {
            openApiCollectReferences(entry, into)
        }
    }
}

/* openApiResolves follows a local json pointer ("#/components/schemas/Name"). A reference into another document is
reported as unresolvable on purpose: the example publishes one self-contained document, so an external reference
there is a generator defect and not a valid style choice. */
func openApiResolves(document map[string]any, reference string) bool {
    if false == strings.HasPrefix(reference, "#/") {
        return false
    }

    var current any = document

    for _, rawSegment := range strings.Split(strings.TrimPrefix(reference, "#/"), "/") {
        segment := openApiUnescapePointerSegment(rawSegment)

        node, isMap := current.(map[string]any)
        if false == isMap {
            return false
        }

        value, exists := node[segment]
        if false == exists {
            return false
        }

        current = value
    }

    return true
}

/* openApiUnescapePointerSegment decodes the two json-pointer escapes; a component name containing a slash is
legal and would otherwise be reported as a dangling reference. */
func openApiUnescapePointerSegment(segment string) string {
    return strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")
}

func openApiSortedKeys(node map[string]any) []string {
    keys := make([]string, 0, len(node))
    for key := range node {
        keys = append(keys, key)
    }

    sort.Strings(keys)

    return keys
}
