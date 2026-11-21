package otlp

import (
    "strconv"

    sdktrace "go.opentelemetry.io/otel/sdk/trace"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

type samplingMode uint8

const (
    samplingModeEvery samplingMode = iota
    samplingModeRatio
    samplingModeNone
)

/* Sampling is the head sampling decision of the tracer provider: its zero value samples every trace, SampleRatio keeps a fraction of them and SampleNone keeps none. The sampler is parent-based: an inbound traceparent's sampled flag decides for the whole trace, so at an untrusted edge the decision is the client's to make; strip or ignore the inbound header there. */
type Sampling struct {
    mode  samplingMode
    ratio float64
}

/* SampleRatio samples the fraction ratio of the traces that have no sampled parent, in (0, 1], 1 sampling every one; 0, a value above 1 and NaN are refused at NewTracerProvider naming Config.Sampling, SampleNone being the one spelling of "none". */
func SampleRatio(ratio float64) Sampling {
    return Sampling{mode: samplingModeRatio, ratio: ratio}
}

/* SampleNone samples no trace that has no sampled parent: the switch an operator needs to stop tracing what starts here without unwiring the module. A request carrying a sampled traceparent is still traced, the sampler being parent-based; an edge that must trace nothing strips the inbound header. */
func SampleNone() Sampling {
    return Sampling{mode: samplingModeNone}
}

func (instance Sampling) String() string {
    switch instance.mode {
    case samplingModeRatio:
        return "ratio(" + strconv.FormatFloat(instance.ratio, 'g', -1, 64) + ")"
    case samplingModeNone:
        return "none"
    }

    return "every"
}

func resolveSampler(sampling Sampling) (sdktrace.Sampler, error) {
    switch sampling.mode {
    case samplingModeNone:
        return sdktrace.NeverSample(), nil
    case samplingModeRatio:
        /* the not-in-range form is deliberate: NaN fails every comparison, so a plain range check would let it through */
        if false == (0 < sampling.ratio && 1 >= sampling.ratio) {
            return nil, exception.NewError(
                "the sample ratio is outside (0, 1]: use SampleNone to sample no trace, or a ratio up to 1 to sample that fraction",
                exceptioncontract.Context{
                    "field": "Config.Sampling",
                    "value": strconv.FormatFloat(sampling.ratio, 'g', -1, 64),
                },
                nil,
            )
        }

        if 1 == sampling.ratio {
            return sdktrace.AlwaysSample(), nil
        }

        return sdktrace.TraceIDRatioBased(sampling.ratio), nil
    }

    return sdktrace.AlwaysSample(), nil
}
