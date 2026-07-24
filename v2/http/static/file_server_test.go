package static

import (
    "net/http"
    "os"
    "strings"
    "testing"
    "testing/fstest"
    "time"

    "github.com/precision-soft/melody/v2/internal/testhelper"
    "github.com/precision-soft/melody/v2/logging"
)

func TestFileServer_Filesystem_ServesFile(t *testing.T) {
    directory := t.TempDir()

    filePath := directory + "/index.html"
    err := osWriteFile(filePath, []byte("hello"))
    if nil != err {
        t.Fatalf("write file error: %v", err)
    }

    config := NewFileServerConfig(
        ModeFilesystem,
        directory,
        "index.html",
        "",
        false,
        0,
        false,
    )

    server := NewFileServer(
        NewOptions(
            config,
            "",
            nil,
        ),
    )

    statusCode, headers, body, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/index.html"),
        logging.NewNopLogger(),
    )

    if false == served {
        t.Fatalf("expected served")
    }

    if 200 != statusCode {
        t.Fatalf("unexpected status")
    }

    if "" == string(body) {
        t.Fatalf("expected body")
    }

    if nil == headers {
        t.Fatalf("expected headers")
    }
}

func TestFileServer_Embedded_ServesFile(t *testing.T) {
    fs := fstest.MapFS{
        "a.txt": &fstest.MapFile{
            Data: []byte("a"),
        },
    }

    config := NewFileServerConfig(
        ModeEmbedded,
        "",
        "",
        "",
        false,
        0,
        false,
    )

    server := NewFileServer(
        NewOptions(
            config,
            "",
            fs,
        ),
    )

    statusCode, _, body, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/a.txt"),
        logging.NewNopLogger(),
    )

    if false == served {
        t.Fatalf("expected served")
    }

    if 200 != statusCode {
        t.Fatalf("unexpected status")
    }

    if "a" != string(body) {
        t.Fatalf("unexpected body")
    }
}

func TestFileServer_DefaultCacheMaxAge_AppliesWhenEnabledAndZero(t *testing.T) {
    fs := fstest.MapFS{
        "a.txt": &fstest.MapFile{
            Data:    []byte("a"),
            ModTime: time.Now(),
        },
    }

    config := NewFileServerConfig(
        ModeEmbedded,
        "",
        "",
        "",
        true,
        0,
        false,
    )

    server := NewFileServer(
        NewOptions(
            config,
            "",
            fs,
        ),
    )

    statusCode, headers, _, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/a.txt"),
        logging.NewNopLogger(),
    )

    if false == served {
        t.Fatalf("expected served")
    }

    if http.StatusOK != statusCode {
        t.Fatalf("unexpected status")
    }

    if nil == headers {
        t.Fatalf("expected headers")
    }

    if "public, max-age=3600" != headers.Get("Cache-Control") {
        t.Fatalf("expected default cache-control max-age=3600")
    }
}

func TestFileServer_Head_ReturnsNoBodyAndSetsContentLength(t *testing.T) {
    fs := fstest.MapFS{
        "a.txt": &fstest.MapFile{
            Data: []byte("a"),
        },
    }

    config := NewFileServerConfig(
        ModeEmbedded,
        "",
        "",
        "",
        false,
        0,
        false,
    )

    server := NewFileServer(
        NewOptions(
            config,
            "",
            fs,
        ),
    )

    statusCode, headers, body, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodHead, "http://example.com/a.txt"),
        logging.NewNopLogger(),
    )

    if false == served {
        t.Fatalf("expected served")
    }

    if 200 != statusCode {
        t.Fatalf("unexpected status")
    }

    if 0 != len(body) {
        t.Fatalf("expected no body for HEAD")
    }

    if "" == headers.Get("Content-Length") {
        t.Fatalf("expected content-length")
    }
}

func TestFileServer_IfModifiedSince_SubSecondModTime_ReturnsNotModified(t *testing.T) {
    modifiedAt := time.Date(2026, 1, 3, 12, 34, 56, 123000000, time.UTC)

    fs := fstest.MapFS{
        "a.txt": &fstest.MapFile{
            Data:    []byte("a"),
            ModTime: modifiedAt,
        },
    }

    config := NewFileServerConfig(
        ModeEmbedded,
        "",
        "",
        "",
        true,
        3600,
        false,
    )

    server := NewFileServer(
        NewOptions(
            config,
            "",
            fs,
        ),
    )

    request := testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/a.txt")
    request.HttpRequest().Header.Set("If-Modified-Since", modifiedAt.Truncate(time.Second).Format(http.TimeFormat))

    statusCode, _, _, served := server.Serve(
        request,
        logging.NewNopLogger(),
    )

    if false == served {
        t.Fatalf("expected served")
    }

    if http.StatusNotModified != statusCode {
        t.Fatalf("expected 304")
    }
}

func TestFileServer_IfNoneMatch_ReturnsNotModified(t *testing.T) {
    modifiedAt := time.Date(2026, 1, 3, 12, 34, 56, 0, time.UTC)

    fs := fstest.MapFS{
        "a.txt": &fstest.MapFile{
            Data:    []byte("a"),
            ModTime: modifiedAt,
        },
    }

    config := NewFileServerConfig(
        ModeEmbedded,
        "",
        "",
        "",
        true,
        3600,
        false,
    )

    server := NewFileServer(
        NewOptions(
            config,
            "",
            fs,
        ),
    )

    firstStatusCode, firstHeaders, _, firstServed := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/a.txt"),
        logging.NewNopLogger(),
    )

    if false == firstServed {
        t.Fatalf("expected served")
    }

    if http.StatusOK != firstStatusCode {
        t.Fatalf("unexpected status")
    }

    etag := firstHeaders.Get("ETag")
    if "" == etag {
        t.Fatalf("expected etag")
    }

    request := testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/a.txt")
    request.HttpRequest().Header.Set("If-None-Match", etag)

    statusCode, _, _, served := server.Serve(
        request,
        logging.NewNopLogger(),
    )

    if false == served {
        t.Fatalf("expected served")
    }

    if http.StatusNotModified != statusCode {
        t.Fatalf("expected 304")
    }
}

func TestContentTypeByExtension_ResolvesIcoAndIsCaseInsensitive(t *testing.T) {
    icoType := contentTypeByExtension(".ico")
    if false == strings.HasPrefix(icoType, "image/") {
        t.Fatalf("expected an image content type for .ico, got %q", icoType)
    }

    if contentTypeByExtension(".ICO") != icoType {
        t.Fatalf("expected case-insensitive resolution for .ICO, got %q want %q", contentTypeByExtension(".ICO"), icoType)
    }

    if "" == contentTypeByExtension(".svg") {
        t.Fatalf("expected a content type for .svg, got empty")
    }
}

func osWriteFile(path string, data []byte) error {
    return os.WriteFile(path, data, 0o644)
}

/* @info the strip prefix leaves a relative remainder, so a leading ".." survives path.Clean and the join with the public directory absorbs it; the request must stay confined to the public directory instead of reaching a sibling of it in the embedded filesystem */
func TestFileServer_Embedded_StripPrefixCannotEscapeThePublicDirectory(t *testing.T) {
    fileSystem := fstest.MapFS{
        "public/index.html": &fstest.MapFile{
            Data: []byte("public-index"),
        },
        "secret.txt": &fstest.MapFile{
            Data: []byte("top-secret"),
        },
        "templates/admin.html": &fstest.MapFile{
            Data: []byte("admin-template"),
        },
    }

    config := NewFileServerConfig(
        ModeEmbedded,
        "public",
        "index.html",
        "/static/",
        false,
        0,
        false,
    )

    server := NewFileServer(
        NewOptions(
            config,
            "",
            fileSystem,
        ),
    )

    for _, requestPath := range []string{
        "http://example.com/static/../secret.txt",
        "http://example.com/static/../templates/admin.html",
        "http://example.com/static/./../secret.txt",
    } {
        _, _, body, served := server.Serve(
            testhelper.NewHttpTestRequest(http.MethodGet, requestPath),
            logging.NewNopLogger(),
        )

        if true == served {
            t.Fatalf("expected %q not to be served, got body %q", requestPath, string(body))
        }
    }

    statusCode, _, body, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/static/index.html"),
        logging.NewNopLogger(),
    )

    if false == served || 200 != statusCode || "public-index" != string(body) {
        t.Fatalf("expected the public file to stay reachable, got served=%v status=%d body=%q", served, statusCode, string(body))
    }
}
