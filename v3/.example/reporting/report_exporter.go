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

const reportExportInstantLayout = time.RFC3339

func NewCatalogReportExporter(exportEndpoint string) *CatalogReportExporter {
    return &CatalogReportExporter{exportEndpoint: exportEndpoint}
}

/* CatalogReportExporter sends readings to an optional absolute endpoint using a client without a base URL. */
type CatalogReportExporter struct {
    exportEndpoint string
}

type exportPayload struct {
    RecordedAt string `json:"recordedAt"`
    Headline   string `json:"headline"`
    Payload    string `json:"payload"`
}

/* Export returns false when no endpoint is configured and an error for any non-2xx response. */
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
