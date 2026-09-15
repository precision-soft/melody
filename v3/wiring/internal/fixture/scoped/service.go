package scoped

const ServiceRequestTrail = "fixture.request_trail"

func NewProcessWriter() *ProcessWriter {
    return &ProcessWriter{}
}

type ProcessWriter struct {
}


//melody:scoped
//melody:service ServiceRequestTrail
func NewRequestTrail(writer *ProcessWriter) (*RequestTrail, error) {
    return &RequestTrail{writer: writer}, nil
}

type RequestTrail struct {
    writer *ProcessWriter
}


//melody:scoped
func NewRequestClock() *RequestClock {
    return &RequestClock{}
}

type RequestClock struct {
}
