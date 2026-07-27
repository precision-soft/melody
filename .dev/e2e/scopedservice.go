package main

import (
    "net/http"
)

type scopedTrailDemo struct {
    RequestId   string   `json:"requestId"`
    SameForBoth bool     `json:"sameForBoth"`
    Entries     []string `json:"entries"`
    Summary     string   `json:"summary"`
}

/* runScopedServiceCheck drives the scope-owned service the example declares through a `//melody:scoped`
constructor. It is the end-to-end half of the codegen: the directive is read by the scanner, emitted into the
generated scoped registration function, registered through the module's RegisterScopedServices hook — and only
here is it proven that what came out of all that is really one instance per request.

The two claims a single response cannot make on its own are made across two: inside one request both
resolutions must yield the same object, and between two requests the instance must not be carried over, which
shows as an entry count that does not grow and a request id that changes. */
func runScopedServiceCheck(baseUrl string) {
    client := newLiveExampleClient(baseUrl)

    firstDemo := requestScopedTrailDemo(client, "scoped service (first request)")

    if false == firstDemo.SameForBoth {
        fail("scoped service: two resolutions inside one request yielded two instances")
    }

    if 2 != len(firstDemo.Entries) {
        fail("scoped service: expected the trail to carry the two entries this request recorded, got %v", firstDemo.Entries)
    }

    pass("both resolutions inside one request yielded the same scope-owned instance")

    secondDemo := requestScopedTrailDemo(client, "scoped service (second request)")

    if firstDemo.RequestId == secondDemo.RequestId {
        fail("scoped service: the two requests reported the same request id (%q), so they shared a scope", firstDemo.RequestId)
    }

    if 2 != len(secondDemo.Entries) {
        fail(
            "scoped service: the second request found %d entries, so the instance the first request built survived it",
            len(secondDemo.Entries),
        )
    }

    pass("a second request built its own instance rather than inheriting the first one's (2 entries, not 4)")

    if "" == secondDemo.Summary {
        fail("scoped service: the trail reported no summary, so the container singleton it takes was not injected")
    }

    pass("the scoped service read both levels: the request context of its own scope and a container singleton")
}

func requestScopedTrailDemo(client *liveExampleClient, label string) scopedTrailDemo {
    response := client.get(label, "/scoped/trail/demo")
    requireLiveExampleStatus(label, "the scoped trail demo", response, http.StatusOK)

    demo := scopedTrailDemo{}
    decodeLiveExamplePayload(label, response, &demo)

    return demo
}
