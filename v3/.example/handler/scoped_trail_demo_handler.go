package handler

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v3/.example/presenter"
    "github.com/precision-soft/melody/v3/.example/reporting"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type scopedTrailDemoPayload struct {
    RequestId   string   `json:"requestId"`
    SameForBoth bool     `json:"sameForBoth"`
    Entries     []string `json:"entries"`
    Summary     string   `json:"summary"`
}

/* ScopedTrailDemoHandler shows what a scope-owned service is. The trail is declared by a `//melody:scoped`
constructor, so the generator registers it through the scoped registrar and the container never sees it.

The handler resolves it twice and reports whether both resolutions yielded the same object: inside one
request they must, because a scope holds one instance of what it owns. Across two requests they must not —
the entry count stays at two however often the route is called, which is the half a single response can
show. */
func ScopedTrailDemoHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        firstTrail, firstErr := melodycontainer.FromResolver[*reporting.RequestReportTrail](
            runtimeInstance.Scope(),
            reporting.ServiceRequestReportTrail,
        )
        if nil != firstErr {
            return nil, firstErr
        }

        firstTrail.Record("resolved the trail")

        secondTrail, secondErr := melodycontainer.FromResolver[*reporting.RequestReportTrail](
            runtimeInstance.Scope(),
            reporting.ServiceRequestReportTrail,
        )
        if nil != secondErr {
            return nil, secondErr
        }

        secondTrail.Record("resolved it again")

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, scopedTrailDemoPayload{
            RequestId:   secondTrail.RequestId(),
            SameForBoth: firstTrail == secondTrail,
            Entries:     secondTrail.Entries(),
            Summary:     secondTrail.Summary(),
        }), nil
    }
}
