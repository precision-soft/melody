package scoped

/* the types below stand in for an application that mixes the two lifetimes: a writer the process owns, and a trail that belongs to one request and is built out of that writer */

const ServiceRequestTrail = "fixture.request_trail"

func NewProcessWriter() *ProcessWriter {
    return &ProcessWriter{}
}

type ProcessWriter struct {
}

/* the request trail carries both directives, so the scoped emission has to survive alongside a declared service name rather than only in the type-only shape */
//melody:scoped
//melody:service ServiceRequestTrail
func NewRequestTrail(writer *ProcessWriter) (*RequestTrail, error) {
    return &RequestTrail{writer: writer}, nil
}

type RequestTrail struct {
    writer *ProcessWriter
}

/* a scoped constructor with no declared service name is registered by type alone */
//melody:scoped
func NewRequestClock() *RequestClock {
    return &RequestClock{}
}

type RequestClock struct {
}

/* the longer word must not be read as the directive: a plain prefix match would claim it and register a process singleton as a per-request service without saying so */
//melody:scopedLater
func NewNearlyScoped() *NearlyScoped {
    return &NearlyScoped{}
}

type NearlyScoped struct {
}
