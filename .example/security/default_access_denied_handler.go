package security

import (
    "html"

    melodyhttp "github.com/precision-soft/melody/http"
    melodyhttpcontract "github.com/precision-soft/melody/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
    melodysecuritycontract "github.com/precision-soft/melody/security/contract"
)

/* NewDefaultAccessDeniedHandler answers a refusal of the session firewall. A browser is answered with a page that says what happened and offers the way out — home, back, and the logout door, since a signed-in account that lands on a page its role cannot open would otherwise be left on a dead end with no way to sign in as another — and an api client with the json refusal. */
func NewDefaultAccessDeniedHandler(homePath string, logoutPath string) melodysecuritycontract.AccessDeniedHandler {
    return &defaultAccessDeniedHandler{
        homePath:   homePath,
        logoutPath: logoutPath,
    }
}

type defaultAccessDeniedHandler struct {
    homePath   string
    logoutPath string
}

func (instance *defaultAccessDeniedHandler) Handle(runtimeInstance melodyruntimecontract.Runtime, request melodyhttpcontract.Request, decisionErr error) (melodyhttpcontract.Response, error) {
    if true == melodyhttp.PrefersHtml(request) {
        return melodyhttp.HtmlResponse(
            403,
            accessDeniedPage(instance.homePath, instance.logoutPath),
        ), nil
    }

    return melodyhttp.JsonErrorResponse(
        403,
        "forbidden",
    ), nil
}

func accessDeniedPage(homePath string, logoutPath string) string {
    return `<!doctype html>
<html lang="en">
<head>
    <meta charset="utf-8"/>
    <meta content="width=device-width, initial-scale=1" name="viewport"/>
    <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.3.8/dist/css/bootstrap.min.css" rel="stylesheet"/>
    <title>Forbidden</title>
</head>
<body class="bg-light">
<div class="container py-5">
    <div class="row justify-content-center">
        <div class="col-md-8 col-lg-6">
            <div class="card shadow-sm">
                <div class="card-body p-4">
                    <h1 class="h3 mb-2">Forbidden</h1>
                    <p class="text-muted mb-4">Your account is signed in, but its role does not open this page.</p>
                    <div class="d-flex flex-wrap gap-2">
                        <a class="btn btn-outline-secondary" href="javascript:history.back()">Back</a>
                        <a class="btn btn-outline-secondary" href="` + html.EscapeString(homePath) + `">Home</a>
                        <a class="btn btn-primary" href="` + html.EscapeString(logoutPath) + `">Log out</a>
                    </div>
                </div>
            </div>
        </div>
    </div>
</div>
</body>
</html>`
}

var _ melodysecuritycontract.AccessDeniedHandler = (*defaultAccessDeniedHandler)(nil)
