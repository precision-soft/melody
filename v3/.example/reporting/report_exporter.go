package reporting

import (
    "time"

    "github.com/precision-soft/melody/v3/.example/service"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/httpclient"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* the instant layout every document of this application publishes; named here so the export and the command
   that prints the same reading cannot drift apart on it */
const reportExportInstantLayout = time.RFC3339

func NewCatalogReportExporter(exportEndpoint string) *CatalogReportExporter {
    return &CatalogReportExporter{exportEndpoint: exportEndpoint}
}

/* CatalogReportExporter pushes a reading to whatever an operator configured to receive it. It is the second
   shape of outbound call this application makes, and deliberately not the first one repeated: the rate
   refresh reads a document from a provider it was configured to trust by base url, while this one WRITES to
   an endpoint whose host the operator chooses, which is why the client it resolves carries no base url and
   the endpoint travels as a whole absolute url.

   With no endpoint configured it does nothing and says so, the shape every optional door here takes. */
type CatalogReportExporter struct {
    exportEndpoint string
}

/* exportPayload is what a sink receives. It is the reading's own fields and nothing else — a sink that wants
   more is asking for a different report, not for a richer envelope. */
type exportPayload struct {
    RecordedAt string `json:"recordedAt"`
    Headline   string `json:"headline"`
    Payload    string `json:"payload"`
}

/* Export answers whether it sent anything. A status outside the success class is an error rather than a
   quiet false: the whole point of an export is that someone downstream received it, so a sink answering
   anything else has to reach the operator through the command's exit code — the same reason the refresh
   refuses to exit zero over a provider it could not read. */
func (instance *CatalogReportExporter) Export(
    runtimeInstance melodyruntimecontract.Runtime,
    reading *CatalogReading,
) (bool, error) {
    if "" == instance.exportEndpoint {
        return false, nil
    }

    if nil == reading {
        return false, exception.NewError("there is no reading to export", nil, nil)
    }

    client, resolveErr := melodycontainer.FromResolver[*httpclient.HttpClient](
        runtimeInstance.Container(),
        service.ServiceReportExportHttpClient,
    )
    if nil != resolveErr {
        return false, resolveErr
    }

    response, requestErr := client.Post(
        instance.exportEndpoint,
        exportPayload{
            RecordedAt: reading.RecordedAt.UTC().Format(reportExportInstantLayout),
            Headline:   reading.Headline,
            Payload:    reading.Payload,
        },
    )
    if nil != requestErr {
        return false, requestErr
    }

    if false == response.IsSuccess() {
        return false, exception.NewError(
            "the report sink refused the export",
            exceptioncontract.Context{
                "status": response.StatusCode(),
            },
            nil,
        )
    }

    return true, nil
}
