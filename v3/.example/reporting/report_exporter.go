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

/* the instant layout every document of this application publishes, shared by the export and the command that prints the same reading */
const reportExportInstantLayout = time.RFC3339

func NewCatalogReportExporter(exportEndpoint string) *CatalogReportExporter {
    return &CatalogReportExporter{exportEndpoint: exportEndpoint}
}

/* CatalogReportExporter pushes a reading to the endpoint an operator configured. The endpoint's host is the operator's choice, so the client it resolves carries no base url and the endpoint travels as a whole absolute url. With no endpoint configured it does nothing and says so. */
type CatalogReportExporter struct {
    exportEndpoint string
}

/* exportPayload is what a sink receives: the reading's own fields and nothing else. */
type exportPayload struct {
    RecordedAt string `json:"recordedAt"`
    Headline   string `json:"headline"`
    Payload    string `json:"payload"`
}

/* Export answers whether it sent anything. A status outside the success class is an error, so a sink that did not receive the reading reaches the operator through the command's exit code. */
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

    /* the client follows no redirect, so a sink that moved answers as its 3xx and is refused, a 307 or 308 included: the address the operator configured is the one they audited */
    if true == isRedirection(response.StatusCode()) {
        return false, exception.NewError(
            "the report sink redirected the export instead of receiving it; the sink is not where it was configured",
            exceptioncontract.Context{
                "status":   response.StatusCode(),
                "location": response.Headers().Get("Location"),
            },
            nil,
        )
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

func isRedirection(statusCode int) bool {
    return 300 <= statusCode && 400 > statusCode
}
