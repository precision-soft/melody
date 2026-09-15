package static

import (
    "bytes"
    "errors"
    "io"
    "io/fs"
    "net/http"
    "os"
    "path"
    "path/filepath"
    "strings"
    "syscall"
    "testing"
    "testing/fstest"
    "time"

    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
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

func TestFileServer_ExplicitZeroCacheMaxAge_IsHonouredAsAlwaysRevalidate(t *testing.T) {
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

    if "public, max-age=0" != headers.Get("Cache-Control") {
        t.Fatalf("expected the explicit zero to be honoured as max-age=0, got %q", headers.Get("Cache-Control"))
    }
}

func TestFileServer_NegativeCacheMaxAge_TakesTheDefault(t *testing.T) {
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
        -1,
        false,
    )

    server := NewFileServer(
        NewOptions(
            config,
            "",
            fs,
        ),
    )

    _, headers, _, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/a.txt"),
        logging.NewNopLogger(),
    )

    if false == served {
        t.Fatalf("expected served")
    }

    if "public, max-age=3600" != headers.Get("Cache-Control") {
        t.Fatalf("expected the negative value to take the 3600 default, got %q", headers.Get("Cache-Control"))
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

func TestFileServer_StripPrefix_ServesFileWhenPrefixMatches(t *testing.T) {
    fs := fstest.MapFS{
        "a.txt": &fstest.MapFile{
            Data: []byte("a"),
        },
    }

    config := NewFileServerConfig(
        ModeEmbedded,
        "",
        "",
        "/static/",
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
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/static/a.txt"),
        logging.NewNopLogger(),
    )

    if false == served {
        t.Fatalf("expected served")
    }

    if 200 != statusCode {
        t.Fatalf("unexpected status: %d", statusCode)
    }

    if "a" != string(body) {
        t.Fatalf("unexpected body: %s", string(body))
    }
}

func TestFileServer_StripPrefix_RejectsWhenPrefixMismatch(t *testing.T) {
    fs := fstest.MapFS{
        "a.txt": &fstest.MapFile{
            Data: []byte("a"),
        },
    }

    config := NewFileServerConfig(
        ModeEmbedded,
        "",
        "",
        "/other/",
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

    _, _, _, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/static/a.txt"),
        logging.NewNopLogger(),
    )

    if true == served {
        t.Fatalf("expected not served when strip prefix mismatches")
    }
}

func TestFileServer_ServesIndexFileForRootPath(t *testing.T) {
    fs := fstest.MapFS{
        "index.html": &fstest.MapFile{
            Data: []byte("hello"),
        },
    }

    config := NewFileServerConfig(
        ModeEmbedded,
        "",
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
            fs,
        ),
    )

    statusCode, _, body, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/"),
        logging.NewNopLogger(),
    )

    if false == served {
        t.Fatalf("expected served for root path")
    }

    if 200 != statusCode {
        t.Fatalf("unexpected status: %d", statusCode)
    }

    if "hello" != string(body) {
        t.Fatalf("expected index file content, got: %s", string(body))
    }
}

func TestFileServer_ReturnsNotServedForMissingFile(t *testing.T) {
    fs := fstest.MapFS{}

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

    _, _, _, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/nonexistent.txt"),
        logging.NewNopLogger(),
    )

    if true == served {
        t.Fatalf("expected not served for missing file")
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

func TestFileServer_Filesystem_DoesNotResolveAWhitespacePaddedPath(t *testing.T) {
    directory := t.TempDir()

    if writeErr := osWriteFile(directory+"/app.css", []byte("body{}")); nil != writeErr {
        t.Fatalf("write file error: %v", writeErr)
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

    statusCode, _, body, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/%20app.css"),
        logging.NewNopLogger(),
    )

    if true == served {
        t.Fatalf("expected the padded spelling not to be served, got status=%d body=%q", statusCode, string(body))
    }

    _, _, controlBody, controlServed := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/app.css"),
        logging.NewNopLogger(),
    )

    if false == controlServed || "body{}" != string(controlBody) {
        t.Fatalf("expected the exact spelling to stay reachable, got served=%v body=%q", controlServed, string(controlBody))
    }
}

func TestFileServer_Embedded_RefusesANonCanonicalPath(t *testing.T) {
    fileSystem := fstest.MapFS{
        "open/note.txt": &fstest.MapFile{
            Data: []byte("open"),
        },
        "internal/secret.json": &fstest.MapFile{
            Data: []byte("secret"),
        },
    }

    config := NewFileServerConfig(
        ModeEmbedded,
        "",
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
            fileSystem,
        ),
    )

    for _, requestPath := range []string{
        "http://example.com/open/../internal/secret.json",
        "http://example.com/internal//secret.json",
        "http://example.com/./internal/secret.json",
        "http://example.com/internal/./secret.json",
        "http://example.com/internal/secret.json/",
        "http://example.com//internal/secret.json",
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
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/internal/secret.json"),
        logging.NewNopLogger(),
    )

    if false == served || http.StatusOK != statusCode || "secret" != string(body) {
        t.Fatalf("expected the canonical spelling to stay reachable, got served=%v status=%d body=%q", served, statusCode, string(body))
    }
}

func TestFileServer_Filesystem_RefusesANonCanonicalPath(t *testing.T) {
    directory := t.TempDir()

    if makeErr := os.MkdirAll(directory+"/internal", 0o755); nil != makeErr {
        t.Fatalf("make directory error: %v", makeErr)
    }

    if writeErr := osWriteFile(directory+"/internal/secret.json", []byte("secret")); nil != writeErr {
        t.Fatalf("write file error: %v", writeErr)
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

    for _, requestPath := range []string{
        "http://example.com/open/../internal/secret.json",
        "http://example.com/internal//secret.json",
        "http://example.com/./internal/secret.json",
        "http://example.com/internal/secret.json/",
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
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/internal/secret.json"),
        logging.NewNopLogger(),
    )

    if false == served || http.StatusOK != statusCode || "secret" != string(body) {
        t.Fatalf("expected the canonical spelling to stay reachable, got served=%v status=%d body=%q", served, statusCode, string(body))
    }
}

func TestFileServer_StripPrefix_ResolvesTheFileFromTheSpellingTheRouterRoutes(t *testing.T) {
    fileSystem := fstest.MapFS{
        "private/secret.txt": &fstest.MapFile{Data: []byte("TOP SECRET")},
        "caf\u00e9.txt":       &fstest.MapFile{Data: []byte("café")},
    }

    config := NewFileServerConfig(
        ModeEmbedded,
        "",
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

    _, _, body, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/static/private%2Fsecret.txt"),
        logging.NewNopLogger(),
    )

    if true == served {
        t.Fatalf("expected the encoded separator not to reach the file beneath it, got body %q", string(body))
    }

    statusCode, _, body, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/static/private/secret.txt"),
        logging.NewNopLogger(),
    )

    if false == served || http.StatusOK != statusCode || "TOP SECRET" != string(body) {
        t.Fatalf("expected the plain spelling to stay reachable, got served=%v status=%d body=%q", served, statusCode, string(body))
    }

    statusCode, _, body, served = server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/static/caf%C3%A9.txt"),
        logging.NewNopLogger(),
    )

    if false == served || http.StatusOK != statusCode || "café" != string(body) {
        t.Fatalf("expected a segment that decodes to no separator to resolve by its decoded spelling, got served=%v status=%d body=%q", served, statusCode, string(body))
    }
}

func TestFileServer_StripPrefix_RefusesANonCanonicalPath(t *testing.T) {
    fileSystem := fstest.MapFS{
        "index.html": &fstest.MapFile{
            Data: []byte("index"),
        },
        "a.txt": &fstest.MapFile{
            Data: []byte("a"),
        },
        "secret.txt": &fstest.MapFile{
            Data: []byte("secret"),
        },
    }

    config := NewFileServerConfig(
        ModeEmbedded,
        "",
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
        "http://example.com/static//a.txt",
        "http://example.com/static/../secret.txt",
        "http://example.com/static/./a.txt",
        "http://example.com/static/a.txt/",
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
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/static/a.txt"),
        logging.NewNopLogger(),
    )

    if false == served || http.StatusOK != statusCode || "a" != string(body) {
        t.Fatalf("expected the canonical spelling to stay reachable, got served=%v status=%d body=%q", served, statusCode, string(body))
    }

    rootStatusCode, _, rootBody, rootServed := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/static/"),
        logging.NewNopLogger(),
    )

    if false == rootServed || http.StatusOK != rootStatusCode || "index" != string(rootBody) {
        t.Fatalf("expected the mount root to keep answering the index file, got served=%v status=%d body=%q", rootServed, rootStatusCode, string(rootBody))
    }
}

func TestFileServer_ServesTheIndexFileForTheMountRoot(t *testing.T) {
    server := newFoldingRootTestFileServer()

    for _, requestPath := range []string{
        "http://example.com/",
    } {
        statusCode, _, body, served := server.Serve(
            testhelper.NewHttpTestRequest(http.MethodGet, requestPath),
            logging.NewNopLogger(),
        )

        if false == served || http.StatusOK != statusCode || "index" != string(body) {
            t.Fatalf("expected %q to answer the index file, got served=%v status=%d body=%q", requestPath, served, statusCode, string(body))
        }
    }
}

func TestFileServer_RefusesTheSpellingsThatFoldIntoTheRoot(t *testing.T) {
    server := newFoldingRootTestFileServer()

    for _, requestPath := range []string{
        "http://example.com/.",
        "http://example.com/..",
        "http://example.com//",
        "http://example.com/open/..",
    } {
        _, _, _, served := server.Serve(
            testhelper.NewHttpTestRequest(http.MethodGet, requestPath),
            logging.NewNopLogger(),
        )

        if true == served {
            t.Fatalf("expected %q to be refused as a non canonical spelling of the mount root", requestPath)
        }
    }
}

func TestFileServer_ServeReaderRefusesTheSpellingsThatFoldIntoTheRoot(t *testing.T) {
    server := newFoldingRootTestFileServer()

    statusCode, _, bodyReader, served := server.ServeReader(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/open/.."),
        logging.NewNopLogger(),
    )

    if true == served || 0 != statusCode || nil != bodyReader {
        t.Fatalf("expected the streaming half to refuse the folded spelling, got served=%v status=%d", served, statusCode)
    }
}

func newFoldingRootTestFileServer() *FileServer {
    fileSystem := fstest.MapFS{
        "index.html": &fstest.MapFile{
            Data: []byte("index"),
        },
    }

    config := NewFileServerConfig(
        ModeEmbedded,
        "",
        "index.html",
        "",
        false,
        0,
        false,
    )

    return NewFileServer(
        NewOptions(
            config,
            "",
            fileSystem,
        ),
    )
}

func TestFileServer_Embedded_RefusesADotPrefixedPathElement(t *testing.T) {
    fileSystem := fstest.MapFS{
        "index.html": &fstest.MapFile{
            Data: []byte("index"),
        },
        ".env": &fstest.MapFile{
            Data: []byte("APP_SECRET=1"),
        },
        ".git/config": &fstest.MapFile{
            Data: []byte("[core]"),
        },
        "assets/.htpasswd": &fstest.MapFile{
            Data: []byte("user:hash"),
        },
    }

    config := NewFileServerConfig(
        ModeEmbedded,
        "",
        "index.html",
        "",
        true,
        3600,
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
        "http://example.com/.env",
        "http://example.com/.git/config",
        "http://example.com/assets/.htpasswd",
    } {
        _, headers, body, served := server.Serve(
            testhelper.NewHttpTestRequest(http.MethodGet, requestPath),
            logging.NewNopLogger(),
        )

        if true == served {
            t.Fatalf("expected %q not to be served, got body %q and cache-control %q", requestPath, string(body), headers.Get("Cache-Control"))
        }
    }

    statusCode, _, body, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/index.html"),
        logging.NewNopLogger(),
    )

    if false == served || http.StatusOK != statusCode || "index" != string(body) {
        t.Fatalf("expected the published file to stay reachable, got served=%v status=%d body=%q", served, statusCode, string(body))
    }
}

func TestFileServer_Filesystem_RefusesADotPrefixedPathElement(t *testing.T) {
    directory := t.TempDir()

    if writeErr := osWriteFile(directory+"/.env", []byte("APP_SECRET=1")); nil != writeErr {
        t.Fatalf("write file error: %v", writeErr)
    }

    if writeErr := osWriteFile(directory+"/index.html", []byte("index")); nil != writeErr {
        t.Fatalf("write file error: %v", writeErr)
    }

    config := NewFileServerConfig(
        ModeFilesystem,
        directory,
        "index.html",
        "",
        true,
        3600,
        false,
    )

    server := NewFileServer(
        NewOptions(
            config,
            "",
            nil,
        ),
    )

    _, headers, body, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/.env"),
        logging.NewNopLogger(),
    )

    if true == served {
        t.Fatalf("expected the dotfile not to be served, got body %q and cache-control %q", string(body), headers.Get("Cache-Control"))
    }

    statusCode, _, controlBody, controlServed := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/index.html"),
        logging.NewNopLogger(),
    )

    if false == controlServed || http.StatusOK != statusCode || "index" != string(controlBody) {
        t.Fatalf("expected the published file to stay reachable, got served=%v status=%d body=%q", controlServed, statusCode, string(controlBody))
    }
}

func TestFileServer_AnswersOnlyRetrievalMethods(t *testing.T) {
    fileSystem := fstest.MapFS{
        "a.txt": &fstest.MapFile{
            Data: []byte("a"),
        },
    }

    config := NewFileServerConfig(
        ModeEmbedded,
        "",
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
            fileSystem,
        ),
    )

    for _, method := range []string{
        http.MethodPost,
        http.MethodPut,
        http.MethodPatch,
        http.MethodDelete,
        http.MethodOptions,
    } {
        _, _, body, served := server.Serve(
            testhelper.NewHttpTestRequest(method, "http://example.com/a.txt"),
            logging.NewNopLogger(),
        )

        if true == served {
            t.Fatalf("expected %s not to be answered from the public directory, got body %q", method, string(body))
        }

        _, _, bodyReader, readerServed := server.ServeReader(
            testhelper.NewHttpTestRequest(method, "http://example.com/a.txt"),
            logging.NewNopLogger(),
        )

        if true == readerServed {
            if nil != bodyReader {
                _ = bodyReader.Close()
            }

            t.Fatalf("expected %s not to be answered from the public directory by the streaming path", method)
        }
    }

    for _, method := range []string{
        http.MethodGet,
        http.MethodHead,
    } {
        statusCode, _, _, served := server.Serve(
            testhelper.NewHttpTestRequest(method, "http://example.com/a.txt"),
            logging.NewNopLogger(),
        )

        if false == served || http.StatusOK != statusCode {
            t.Fatalf("expected %s to be answered, got served=%v status=%d", method, served, statusCode)
        }
    }
}

func TestFileServer_IgnoresIfModifiedSinceWhenIfNoneMatchIsPresent(t *testing.T) {
    modifiedAt := time.Date(2026, 1, 3, 12, 34, 56, 0, time.UTC)

    fileSystem := fstest.MapFS{
        "a.txt": &fstest.MapFile{
            Data:    []byte("a"),
            ModTime: modifiedAt,
        },
    }

    config := NewFileServerConfig(
        ModeEmbedded,
        "",
        "index.html",
        "",
        true,
        3600,
        false,
    )

    server := NewFileServer(
        NewOptions(
            config,
            "",
            fileSystem,
        ),
    )

    request := testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/a.txt")
    request.HttpRequest().Header.Set("If-None-Match", "\"0-0\"")
    request.HttpRequest().Header.Set("If-Modified-Since", modifiedAt.Format(http.TimeFormat))

    statusCode, _, body, served := server.Serve(
        request,
        logging.NewNopLogger(),
    )

    if false == served {
        t.Fatalf("expected served")
    }

    if http.StatusOK != statusCode || "a" != string(body) {
        t.Fatalf("expected the body to be re-sent when the offered entity tag does not match, got status=%d body=%q", statusCode, string(body))
    }
}

func TestFileServer_AcceptsEveryHttpDateFormatForIfModifiedSince(t *testing.T) {
    modifiedAt := time.Date(2026, 1, 3, 12, 34, 56, 0, time.UTC)

    fileSystem := fstest.MapFS{
        "a.txt": &fstest.MapFile{
            Data:    []byte("a"),
            ModTime: modifiedAt,
        },
    }

    config := NewFileServerConfig(
        ModeEmbedded,
        "",
        "index.html",
        "",
        true,
        3600,
        false,
    )

    server := NewFileServer(
        NewOptions(
            config,
            "",
            fileSystem,
        ),
    )

    for _, layout := range []string{
        http.TimeFormat,
        time.RFC850,
        time.ANSIC,
    } {
        request := testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/a.txt")
        request.HttpRequest().Header.Set("If-Modified-Since", modifiedAt.Format(layout))

        statusCode, _, _, served := server.Serve(
            request,
            logging.NewNopLogger(),
        )

        if false == served {
            t.Fatalf("expected served")
        }

        if http.StatusNotModified != statusCode {
            t.Fatalf("expected 304 for the date written as %q, got %d", modifiedAt.Format(layout), statusCode)
        }
    }
}

func TestFileServer_ServeReader_LogsTheRefusedResolution(t *testing.T) {
    fileSystem := fstest.MapFS{
        "internal/secret.json": &fstest.MapFile{
            Data: []byte("secret"),
        },
        ".env": &fstest.MapFile{
            Data: []byte("APP_SECRET=1"),
        },
    }

    config := NewFileServerConfig(
        ModeEmbedded,
        "",
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
            fileSystem,
        ),
    )

    for _, expectation := range []struct {
        requestPath string
        message     string
    }{
        {
            requestPath: "http://example.com/missing.txt",
            message:     "static serve open failed",
        },
        {
            requestPath: "http://example.com/open/../internal/secret.json",
            message:     "static serve non canonical path",
        },
        {
            requestPath: "http://example.com/.env",
            message:     "static serve dot prefixed path element",
        },
    } {
        output := &bytes.Buffer{}

        _, _, bodyReader, served := server.ServeReader(
            testhelper.NewHttpTestRequest(http.MethodGet, expectation.requestPath),
            logging.NewJsonLogger(output, loggingcontract.LevelDebug),
        )

        if true == served {
            if nil != bodyReader {
                _ = bodyReader.Close()
            }

            t.Fatalf("expected %q not to be served", expectation.requestPath)
        }

        if false == strings.Contains(output.String(), expectation.message) {
            t.Fatalf("expected the streaming resolution to log %q for %q, got: %s", expectation.message, expectation.requestPath, output.String())
        }
    }
}
func TestFileServer_Embedded_RetrievesTheAllowedDotPrefixOnly(t *testing.T) {
    fileSystem := fstest.MapFS{
        "index.html": &fstest.MapFile{
            Data: []byte("index"),
        },
        ".well-known/acme-challenge/token": &fstest.MapFile{
            Data: []byte("challenge"),
        },
        ".well-known/.env": &fstest.MapFile{
            Data: []byte("APP_SECRET=1"),
        },
        ".env": &fstest.MapFile{
            Data: []byte("APP_SECRET=1"),
        },
        "assets/.well-known/token": &fstest.MapFile{
            Data: []byte("nested"),
        },
    }

    config := NewFileServerConfig(
        ModeEmbedded,
        "",
        "index.html",
        "",
        true,
        3600,
        false,
    )

    server := NewFileServer(
        NewOptions(
            config,
            "",
            fileSystem,
        ),
    )

    _, _, body, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/.well-known/acme-challenge/token"),
        logging.NewNopLogger(),
    )

    if false == served {
        t.Fatalf("expected the well-known challenge to be retrievable so certificate renewal keeps working")
    }

    if "challenge" != string(body) {
        t.Fatalf("expected the challenge body, got %q", string(body))
    }

    for _, requestPath := range []string{
        "http://example.com/.env",
        "http://example.com/.well-known/.env",
        "http://example.com/assets/.well-known/token",
    } {
        _, _, refusedBody, refusedServed := server.Serve(
            testhelper.NewHttpTestRequest(http.MethodGet, requestPath),
            logging.NewNopLogger(),
        )

        if true == refusedServed {
            t.Fatalf("expected %q not to be served, got body %q", requestPath, string(refusedBody))
        }
    }

    config.SetAllowedDotPrefixList(nil)

    clearedServer := NewFileServer(
        NewOptions(
            config,
            "",
            fileSystem,
        ),
    )

    _, _, _, servedAfterClearing := clearedServer.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/.well-known/acme-challenge/token"),
        logging.NewNopLogger(),
    )

    if true == servedAfterClearing {
        t.Fatalf("expected an empty allowance to refuse every dot-prefixed path")
    }
}

func TestFileServer_ExcludedPathPrefixIsDeclinedWithoutReadingTheDisk(t *testing.T) {
    directory := t.TempDir()

    if err := osWriteFile(directory+"/index.html", []byte("public")); nil != err {
        t.Fatalf("write file error: %v", err)
    }

    if err := os.MkdirAll(directory+"/private", 0o750); nil != err {
        t.Fatalf("make directory error: %v", err)
    }

    if err := osWriteFile(directory+"/private/report.json", []byte(`{"secret":true}`)); nil != err {
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

    config.SetExcludedPathList([]string{"/private"})

    server := NewFileServer(
        NewOptions(
            config,
            "",
            nil,
        ),
    )

    _, _, body, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/private/report.json"),
        logging.NewNopLogger(),
    )

    if true == served {
        t.Fatalf("expected the excluded prefix to be declined so the rest of the chain answers, got body %q", string(body))
    }

    statusCode, _, publicBody, publicServed := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/index.html"),
        logging.NewNopLogger(),
    )

    if false == publicServed {
        t.Fatalf("expected a path outside the exclusion to keep being served")
    }

    if 200 != statusCode {
        t.Fatalf("expected status 200, got %d", statusCode)
    }

    if "public" != string(publicBody) {
        t.Fatalf("expected the public body, got %q", string(publicBody))
    }
}

func TestFileServer_ExcludedPathPrefixIsDeclinedByTheStreamingResolution(t *testing.T) {
    directory := t.TempDir()

    if err := os.MkdirAll(directory+"/private", 0o750); nil != err {
        t.Fatalf("make directory error: %v", err)
    }

    if err := osWriteFile(directory+"/private/report.json", []byte(`{"secret":true}`)); nil != err {
        t.Fatalf("write file error: %v", err)
    }

    if err := osWriteFile(directory+"/open.json", []byte(`{"secret":false}`)); nil != err {
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

    config.SetExcludedPathList([]string{"/private"})

    server := NewFileServer(
        NewOptions(
            config,
            "",
            nil,
        ),
    )

    _, _, reader, served := server.ServeReader(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/private/report.json"),
        logging.NewNopLogger(),
    )

    if true == served {
        _ = reader.Close()

        t.Fatalf("expected the streaming resolution to decline the excluded prefix as well")
    }

    _, _, openReader, openServed := server.ServeReader(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/open.json"),
        logging.NewNopLogger(),
    )

    if false == openServed {
        t.Fatalf("expected a path outside the exclusion to keep being streamed")
    }

    _ = openReader.Close()
}

func TestFileServer_ExcludedPathPrefixIsComparedBeforeTheStripPrefixIsRemoved(t *testing.T) {
    directory := t.TempDir()

    if err := os.MkdirAll(directory+"/private", 0o750); nil != err {
        t.Fatalf("make directory error: %v", err)
    }

    if err := osWriteFile(directory+"/private/report.json", []byte(`{"secret":true}`)); nil != err {
        t.Fatalf("write file error: %v", err)
    }

    config := NewFileServerConfig(
        ModeFilesystem,
        directory,
        "index.html",
        "/static",
        false,
        0,
        false,
    )

    config.SetExcludedPathList([]string{"/static/private"})

    server := NewFileServer(
        NewOptions(
            config,
            "",
            nil,
        ),
    )

    _, _, body, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/static/private/report.json"),
        logging.NewNopLogger(),
    )

    if true == served {
        t.Fatalf("expected the mounted exclusion to be declined, got body %q", string(body))
    }
}

func TestFileServer_ExcludedPathListDefaultsToExcludingNothing(t *testing.T) {
    directory := t.TempDir()

    if err := osWriteFile(directory+"/index.html", []byte("public")); nil != err {
        t.Fatalf("write file error: %v", err)
    }

    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(
                ModeFilesystem,
                directory,
                "index.html",
                "",
                false,
                0,
                false,
            ),
            "",
            nil,
        ),
    )

    _, _, _, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/index.html"),
        logging.NewNopLogger(),
    )

    if false == served {
        t.Fatalf("expected the default configuration to exclude nothing")
    }
}

type refusingFileSystem struct {
    openErr error
}

func (instance *refusingFileSystem) Open(name string) (fs.File, error) {
    return nil, instance.openErr
}

type levelRecordingLogger struct {
    loggingcontract.Logger
    warningMessages []string
    debugMessages   []string
    infoMessages    []string
}

func (instance *levelRecordingLogger) Warning(message string, context exceptioncontract.Context) {
    instance.warningMessages = append(instance.warningMessages, message)
}

func (instance *levelRecordingLogger) Debug(message string, context exceptioncontract.Context) {
    instance.debugMessages = append(instance.debugMessages, message)
}

func newRefusingFileServer(openErr error) *FileServer {
    return &FileServer{
        config: NewFileServerConfig(
            ModeFilesystem,
            "/does-not-matter",
            "index.html",
            "",
            false,
            0,
            false,
        ),
        fileSystem: &refusingFileSystem{openErr: openErr},
    }
}

func TestFileServer_AnEscapeRefusalIsRecordedAtWarningRatherThanDebug(t *testing.T) {
    logger := &levelRecordingLogger{Logger: logging.NewNopLogger()}

    server := newRefusingFileServer(fs.ErrPermission)

    _, _, _, _, served := server.serveForStreaming(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/app.css"),
        logger,
    )

    if true == served {
        t.Fatal("expected the refused path not to be served")
    }

    if 1 != len(logger.warningMessages) {
        t.Fatalf("expected the escape refusal to be recorded at warning, got warnings=%v debug=%v", logger.warningMessages, logger.debugMessages)
    }

    if false == strings.Contains(logger.warningMessages[0], "outside the served directory") {
        t.Fatalf("expected the warning to name what was refused, got %q", logger.warningMessages[0])
    }
}

func TestFileServer_AnOrdinaryMissStaysAtDebug(t *testing.T) {
    logger := &levelRecordingLogger{Logger: logging.NewNopLogger()}

    server := newRefusingFileServer(fs.ErrNotExist)

    _, _, _, _, served := server.serveForStreaming(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/app.css"),
        logger,
    )

    if true == served {
        t.Fatal("expected the missing path not to be served")
    }

    if 0 != len(logger.warningMessages) {
        t.Fatalf("expected no warning for an ordinary miss, got %v", logger.warningMessages)
    }

    missRecorded := false
    for _, message := range logger.debugMessages {
        if true == strings.Contains(message, "static serve open failed") {
            missRecorded = true
        }
    }

    if false == missRecorded {
        t.Fatalf("expected the miss to be recorded at debug, got %v", logger.debugMessages)
    }
}


func TestFileServer_RootDoesNotServeAnExcludedIndexFile(t *testing.T) {
    fs := fstest.MapFS{
        "index.html": &fstest.MapFile{Data: []byte("index")},
    }

    config := NewFileServerConfig(
        ModeEmbedded,
        "",
        "index.html",
        "",
        false,
        0,
        false,
    )
    config.SetExcludedPathList([]string{"/index.html"})

    server := NewFileServer(NewOptions(config, "", fs))

    _, _, _, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/"),
        logging.NewNopLogger(),
    )
    if true == served {
        t.Fatalf("expected the root not to serve an index file the exclusion list names")
    }

    _, _, _, _, servedStreaming := server.serveForStreaming(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/"),
        logging.NewNopLogger(),
    )
    if true == servedStreaming {
        t.Fatalf("expected the streaming root not to serve an index file the exclusion list names")
    }
}

func TestFileServer_RootStillServesTheIndexFileWhenNotExcluded(t *testing.T) {
    fs := fstest.MapFS{
        "index.html": &fstest.MapFile{Data: []byte("index")},
    }

    config := NewFileServerConfig(
        ModeEmbedded,
        "",
        "index.html",
        "",
        false,
        0,
        false,
    )
    config.SetExcludedPathList([]string{"/admin/"})

    server := NewFileServer(NewOptions(config, "", fs))

    statusCode, _, body, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/"),
        logging.NewNopLogger(),
    )
    if false == served {
        t.Fatalf("expected the root to keep serving an index file the exclusion list does not name")
    }

    if http.StatusOK != statusCode {
        t.Fatalf("expected 200 for the root index, got %d", statusCode)
    }

    if "index" != string(body) {
        t.Fatalf("expected the index body, got %q", string(body))
    }
}

type trackingFileSystem struct {
    inner       fs.FS
    closedCount int
}

func (instance *trackingFileSystem) Open(name string) (fs.File, error) {
    file, err := instance.inner.Open(name)
    if nil != err {
        return nil, err
    }

    return &trackingFile{File: file, fileSystem: instance}, nil
}

type trackingFile struct {
    fs.File
    fileSystem *trackingFileSystem
}

func (instance *trackingFile) Close() error {
    instance.fileSystem.closedCount++

    return instance.File.Close()
}

type statFailingFileSystem struct {
    statErr     error
    closedCount int
}

func (instance *statFailingFileSystem) Open(name string) (fs.File, error) {
    return &statFailingFile{fileSystem: instance}, nil
}

type statFailingFile struct {
    fileSystem *statFailingFileSystem
}

func (instance *statFailingFile) Stat() (fs.FileInfo, error) {
    return nil, instance.fileSystem.statErr
}

func (instance *statFailingFile) Read(buffer []byte) (int, error) {
    return 0, io.EOF
}

func (instance *statFailingFile) Close() error {
    instance.fileSystem.closedCount++

    return nil
}

type readFailingFileSystem struct {
    readErr error
}

func (instance *readFailingFileSystem) Open(name string) (fs.File, error) {
    return &readFailingFile{readErr: instance.readErr}, nil
}

type readFailingFile struct {
    readErr error
}

func (instance *readFailingFile) Stat() (fs.FileInfo, error) {
    return &readFailingFileInfo{}, nil
}

func (instance *readFailingFile) Read(buffer []byte) (int, error) {
    return 0, instance.readErr
}

func (instance *readFailingFile) Close() error {
    return nil
}

type readFailingFileInfo struct{}

func (instance *readFailingFileInfo) Name() string { return "a.txt" }

func (instance *readFailingFileInfo) Size() int64 { return 5 }

func (instance *readFailingFileInfo) Mode() fs.FileMode { return 0o644 }

func (instance *readFailingFileInfo) ModTime() time.Time { return time.Unix(0, 0).UTC() }

func (instance *readFailingFileInfo) IsDir() bool { return false }

func (instance *readFailingFileInfo) Sys() any { return nil }

func TestNewFileServer_AnUnnamedPublicDirectoryDefaultsToPublic(t *testing.T) {
    directory := t.TempDir()

    makeDirErr := os.MkdirAll(filepath.Join(directory, "public"), 0o755)
    if nil != makeDirErr {
        t.Fatalf("make directory error: %v", makeDirErr)
    }

    writeErr := osWriteFile(filepath.Join(directory, "public", "a.txt"), []byte("a"))
    if nil != writeErr {
        t.Fatalf("write file error: %v", writeErr)
    }

    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(
                ModeFilesystem,
                "",
                "index.html",
                "",
                false,
                0,
                false,
            ),
            directory,
            nil,
        ),
    )

    _, _, body, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/a.txt"),
        logging.NewNopLogger(),
    )

    if false == served {
        t.Fatalf("expected an unnamed public directory to resolve to \"public\"")
    }

    if "a" != string(body) {
        t.Fatalf("expected the body out of the default public directory, got %q", string(body))
    }
}

func TestNewFileServer_AnUnnamedRootAnchorsARelativePublicDirectoryWhereTheProcessRuns(t *testing.T) {
    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(
                ModeFilesystem,
                "assets",
                "index.html",
                "",
                false,
                0,
                false,
            ),
            "",
            nil,
        ),
    )

    directoryFileSystem, ok := server.fileSystem.(*dirFileSystem)
    if false == ok {
        t.Fatalf("expected the filesystem mode to build a directory filesystem, got %T", server.fileSystem)
    }

    if "assets" != directoryFileSystem.basePath {
        t.Fatalf("expected an unnamed root to anchor the relative public directory where the process runs, got %q", directoryFileSystem.basePath)
    }

    anchored := NewFileServer(
        NewOptions(
            NewFileServerConfig(
                ModeFilesystem,
                "assets",
                "index.html",
                "",
                false,
                0,
                false,
            ),
            "/srv/application",
            nil,
        ),
    )

    anchoredFileSystem, ok := anchored.fileSystem.(*dirFileSystem)
    if false == ok {
        t.Fatalf("expected the filesystem mode to build a directory filesystem, got %T", anchored.fileSystem)
    }

    if filepath.Join("/srv/application", "assets") != anchoredFileSystem.basePath {
        t.Fatalf("expected a named root to anchor the relative public directory, got %q", anchoredFileSystem.basePath)
    }
}

func TestNewFileServer_ANilFileSystemIsRefused(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            NewFileServer(
                NewOptions(
                    NewFileServerConfig(
                        ModeEmbedded,
                        "",
                        "index.html",
                        "",
                        false,
                        0,
                        false,
                    ),
                    "",
                    nil,
                ),
            )
        },
        "file system may not be nil for the file server",
    )
}

func TestFileServer_ServeReader_ANilRequestIsRefusedAndRecorded(t *testing.T) {
    logger := &levelRecordingLogger{Logger: logging.NewNopLogger()}

    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(
                ModeEmbedded,
                "",
                "index.html",
                "",
                false,
                0,
                false,
            ),
            "",
            fstest.MapFS{},
        ),
    )

    statusCode, headers, bodyReader, served := server.ServeReader(nil, logger)

    if true == served {
        t.Fatalf("expected a nil request not to be served")
    }

    if 0 != statusCode || nil != headers || nil != bodyReader {
        t.Fatalf("expected an empty refusal, got status=%d headers=%v reader=%v", statusCode, headers, bodyReader)
    }

    if 1 != len(logger.warningMessages) || false == strings.Contains(logger.warningMessages[0], "static serve reader skipped because request is nil") {
        t.Fatalf("expected the streaming door to record the nil request, got %v", logger.warningMessages)
    }
}

func TestFileServer_Serve_ANilRequestIsRefusedAndRecorded(t *testing.T) {
    logger := &levelRecordingLogger{Logger: logging.NewNopLogger()}

    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(
                ModeEmbedded,
                "",
                "index.html",
                "",
                false,
                0,
                false,
            ),
            "",
            fstest.MapFS{},
        ),
    )

    statusCode, headers, body, served := server.Serve(nil, logger)

    if true == served {
        t.Fatalf("expected a nil request not to be served")
    }

    if 0 != statusCode || nil != headers || nil != body {
        t.Fatalf("expected an empty refusal, got status=%d headers=%v body=%v", statusCode, headers, body)
    }

    if 1 != len(logger.warningMessages) || false == strings.Contains(logger.warningMessages[0], "static serve skipped because request is nil") {
        t.Fatalf("expected the buffered door to record the nil request, got %v", logger.warningMessages)
    }
}

func TestFileServer_TheResolutionRefusesANilRequestWithoutRecordingItTwice(t *testing.T) {
    logger := &levelRecordingLogger{Logger: logging.NewNopLogger()}

    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(
                ModeEmbedded,
                "",
                "index.html",
                "",
                false,
                0,
                false,
            ),
            "",
            fstest.MapFS{},
        ),
    )

    statusCode, headers, file, fileInfo, served := server.serveForStreaming(nil, logger)

    if true == served {
        t.Fatalf("expected a nil request not to be resolved")
    }

    if 0 != statusCode || nil != headers || nil != file || nil != fileInfo {
        t.Fatalf("expected an empty refusal, got status=%d headers=%v file=%v info=%v", statusCode, headers, file, fileInfo)
    }

    if 0 != len(logger.warningMessages) || 0 != len(logger.debugMessages) {
        t.Fatalf("expected the resolution to leave the record to the door above it, got warnings=%v debug=%v", logger.warningMessages, logger.debugMessages)
    }
}

func TestFileServer_ServeReader_HeadAnswersTheLengthWithoutABodyAndClosesTheFile(t *testing.T) {
    fileSystem := &trackingFileSystem{
        inner: fstest.MapFS{
            "a.txt": &fstest.MapFile{
                Data: []byte("hello"),
            },
        },
    }

    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(
                ModeEmbedded,
                "",
                "index.html",
                "",
                false,
                0,
                false,
            ),
            "",
            fileSystem,
        ),
    )

    statusCode, headers, bodyReader, served := server.ServeReader(
        testhelper.NewHttpTestRequest(http.MethodHead, "http://example.com/a.txt"),
        logging.NewNopLogger(),
    )

    if false == served {
        t.Fatalf("expected the head request to be answered")
    }

    if http.StatusOK != statusCode {
        t.Fatalf("expected 200 for a head request, got %d", statusCode)
    }

    if nil != bodyReader {
        t.Fatalf("expected no body for a head request")
    }

    if "5" != headers.Get("Content-Length") {
        t.Fatalf("expected the length of the file it did not send, got %q", headers.Get("Content-Length"))
    }

    if 1 != fileSystem.closedCount {
        t.Fatalf("expected the head answer to close the file it opened, closed %d", fileSystem.closedCount)
    }
}

func TestFileServer_ServeReader_ANotModifiedAnswerCarriesNoBody(t *testing.T) {
    modifiedAt := time.Date(2026, 1, 3, 12, 34, 56, 0, time.UTC)

    fileSystem := &trackingFileSystem{
        inner: fstest.MapFS{
            "a.txt": &fstest.MapFile{
                Data:    []byte("a"),
                ModTime: modifiedAt,
            },
        },
    }

    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(
                ModeEmbedded,
                "",
                "index.html",
                "",
                true,
                3600,
                false,
            ),
            "",
            fileSystem,
        ),
    )

    etag := GenerateEtag(&staticEtagFileInfo{size: 1, modTime: modifiedAt}, false)

    request := testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/a.txt")
    request.HttpRequest().Header.Set("If-None-Match", etag)

    statusCode, headers, bodyReader, served := server.ServeReader(request, logging.NewNopLogger())

    if false == served {
        t.Fatalf("expected the conditional request to be answered")
    }

    if http.StatusNotModified != statusCode {
        t.Fatalf("expected 304, got %d", statusCode)
    }

    if nil != bodyReader {
        t.Fatalf("expected no body with a 304")
    }

    if etag != headers.Get("ETag") {
        t.Fatalf("expected the 304 to carry the tag it matched, got %q", headers.Get("ETag"))
    }

    if 1 != fileSystem.closedCount {
        t.Fatalf("expected the 304 to close the file it opened, closed %d", fileSystem.closedCount)
    }
}

func TestFileServer_TheResolutionAlwaysHandsBackAHeaderMap(t *testing.T) {
    modifiedAt := time.Date(2026, 1, 3, 12, 34, 56, 0, time.UTC)

    fileSystem := fstest.MapFS{
        "a.txt": &fstest.MapFile{
            Data:    []byte("a"),
            ModTime: modifiedAt,
        },
    }

    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(
                ModeEmbedded,
                "",
                "index.html",
                "",
                true,
                3600,
                false,
            ),
            "",
            fileSystem,
        ),
    )

    etag := GenerateEtag(&staticEtagFileInfo{size: 1, modTime: modifiedAt}, false)

    conditional := testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/a.txt")
    conditional.HttpRequest().Header.Set("If-None-Match", etag)

    for _, probe := range []struct {
        name    string
        request httpcontract.Request
    }{
        {name: "retrieval", request: testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/a.txt")},
        {name: "conditional", request: conditional},
    } {
        statusCode, headers, file, _, served := server.serveForStreaming(probe.request, logging.NewNopLogger())

        if false == served {
            t.Fatalf("expected the %s probe to resolve", probe.name)
        }

        if nil != file {
            _ = file.Close()
        }

        if nil == headers {
            t.Fatalf("expected the %s probe to carry a header map, status %d", probe.name, statusCode)
        }
    }
}

var _ io.ReadCloser = fs.File(nil)

func TestFileServer_ServeReader_EveryResolvedFileIsAReadCloser(t *testing.T) {
    fileSystem := fstest.MapFS{
        "a.txt": &fstest.MapFile{
            Data: []byte("a"),
        },
    }

    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(
                ModeEmbedded,
                "",
                "index.html",
                "",
                false,
                0,
                false,
            ),
            "",
            fileSystem,
        ),
    )

    _, _, file, _, served := server.serveForStreaming(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/a.txt"),
        logging.NewNopLogger(),
    )

    if false == served {
        t.Fatalf("expected the file to resolve")
    }

    readCloser, ok := file.(io.ReadCloser)
    if false == ok {
        t.Fatalf("expected every resolved file to be a read closer, got %T", file)
    }

    _ = readCloser.Close()
}

func TestFileServer_TheFoldingSpellingsCleanToTheRootRatherThanARelativeName(t *testing.T) {
    for _, spelling := range []string{"/", "//", "/.", "/..", "/./", "/../..", "/a/..", "///.//.."} {
        cleaned := path.Clean(spelling)

        if "." == cleaned || "" == cleaned {
            t.Fatalf("expected %q to fold onto an absolute path, got %q", spelling, cleaned)
        }

        if false == strings.HasPrefix(cleaned, "/") {
            t.Fatalf("expected %q to stay absolute, got %q", spelling, cleaned)
        }
    }
}

func TestFileServer_Serve_AnIndexFileThatLeavesThePublicDirectoryIsRefused(t *testing.T) {
    logger := &levelRecordingLogger{Logger: logging.NewNopLogger()}

    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(
                ModeEmbedded,
                "",
                "../secret",
                "",
                false,
                0,
                false,
            ),
            "",
            fstest.MapFS{},
        ),
    )

    _, _, _, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/"),
        logger,
    )

    if true == served {
        t.Fatalf("expected an index file that leaves the public directory to be refused")
    }

    if false == recordedContains(logger.warningMessages, "static serve invalid relative path") {
        t.Fatalf("expected the refusal to be recorded, got %v", logger.warningMessages)
    }
}

func TestFileServer_ServeReader_AnIndexFileThatLeavesThePublicDirectoryIsRefused(t *testing.T) {
    logger := &levelRecordingLogger{Logger: logging.NewNopLogger()}

    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(
                ModeEmbedded,
                "",
                "../secret",
                "",
                false,
                0,
                false,
            ),
            "",
            fstest.MapFS{},
        ),
    )

    _, _, _, _, served := server.serveForStreaming(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/"),
        logger,
    )

    if true == served {
        t.Fatalf("expected an index file that leaves the public directory to be refused")
    }

    if false == recordedContains(logger.warningMessages, "static serve invalid relative path") {
        t.Fatalf("expected the refusal to be recorded, got %v", logger.warningMessages)
    }
}

func TestFileServer_Serve_AFileThatCannotBeDescribedIsNotServed(t *testing.T) {
    fileSystem := &statFailingFileSystem{statErr: errors.New("stat refused")}

    server := &FileServer{
        config: NewFileServerConfig(
            ModeEmbedded,
            "",
            "index.html",
            "",
            false,
            0,
            false,
        ),
        fileSystem: fileSystem,
    }

    statusCode, _, _, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/a.txt"),
        logging.NewNopLogger(),
    )

    if true == served {
        t.Fatalf("expected a file that cannot be described not to be served, got status %d", statusCode)
    }
}

func TestFileServer_ServeReader_AFileThatCannotBeDescribedIsNotServedAndIsClosed(t *testing.T) {
    fileSystem := &statFailingFileSystem{statErr: errors.New("stat refused")}

    server := &FileServer{
        config: NewFileServerConfig(
            ModeEmbedded,
            "",
            "index.html",
            "",
            false,
            0,
            false,
        ),
        fileSystem: fileSystem,
    }

    _, _, _, _, served := server.serveForStreaming(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/a.txt"),
        logging.NewNopLogger(),
    )

    if true == served {
        t.Fatalf("expected a file that cannot be described not to be served")
    }

    if 1 != fileSystem.closedCount {
        t.Fatalf("expected the refusal to close the file it opened, closed %d", fileSystem.closedCount)
    }
}

func TestFileServer_Serve_ADirectoryIsNotServed(t *testing.T) {
    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(
                ModeEmbedded,
                "",
                "index.html",
                "",
                false,
                0,
                false,
            ),
            "",
            fstest.MapFS{
                "assets/a.txt": &fstest.MapFile{
                    Data: []byte("a"),
                },
            },
        ),
    )

    _, _, _, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/assets"),
        logging.NewNopLogger(),
    )

    if true == served {
        t.Fatalf("expected a directory not to be served")
    }
}

func TestFileServer_ServeReader_ADirectoryIsNotServedAndIsClosed(t *testing.T) {
    fileSystem := &trackingFileSystem{
        inner: fstest.MapFS{
            "assets/a.txt": &fstest.MapFile{
                Data: []byte("a"),
            },
        },
    }

    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(
                ModeEmbedded,
                "",
                "index.html",
                "",
                false,
                0,
                false,
            ),
            "",
            fileSystem,
        ),
    )

    _, _, _, _, served := server.serveForStreaming(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/assets"),
        logging.NewNopLogger(),
    )

    if true == served {
        t.Fatalf("expected a directory not to be served")
    }

    if 1 != fileSystem.closedCount {
        t.Fatalf("expected the refusal to close the directory it opened, closed %d", fileSystem.closedCount)
    }
}

func TestFileServer_Serve_AFileThatCannotBeReadAnswersInternalServerError(t *testing.T) {
    server := &FileServer{
        config: NewFileServerConfig(
            ModeEmbedded,
            "",
            "index.html",
            "",
            false,
            0,
            false,
        ),
        fileSystem: &readFailingFileSystem{readErr: errors.New("read refused")},
    }

    statusCode, headers, body, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/a.txt"),
        logging.NewNopLogger(),
    )

    if false == served {
        t.Fatalf("expected a read failure to be claimed rather than declined")
    }

    if http.StatusInternalServerError != statusCode {
        t.Fatalf("expected 500 for a file that cannot be read, got %d", statusCode)
    }

    if nil != headers || nil != body {
        t.Fatalf("expected the failed read to carry neither headers nor body, got headers=%v body=%v", headers, body)
    }
}

func TestFileServer_ServeReader_StripPrefixServesTheFileBeneathTheMount(t *testing.T) {
    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(
                ModeEmbedded,
                "",
                "index.html",
                "/static/",
                false,
                0,
                false,
            ),
            "",
            fstest.MapFS{
                "a.txt": &fstest.MapFile{
                    Data: []byte("a"),
                },
            },
        ),
    )

    statusCode, _, bodyReader, served := server.ServeReader(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/static/a.txt"),
        logging.NewNopLogger(),
    )

    if false == served {
        t.Fatalf("expected the file beneath the mount to be served")
    }

    if http.StatusOK != statusCode {
        t.Fatalf("expected 200, got %d", statusCode)
    }

    content, readErr := io.ReadAll(bodyReader)
    if nil != readErr {
        t.Fatalf("read error: %v", readErr)
    }

    _ = bodyReader.Close()

    if "a" != string(content) {
        t.Fatalf("expected the body beneath the mount, got %q", string(content))
    }
}

func TestFileServer_ServeReader_TheMountPointAloneResolvesToTheIndexFile(t *testing.T) {
    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(
                ModeEmbedded,
                "",
                "index.html",
                "/static/",
                false,
                0,
                false,
            ),
            "",
            fstest.MapFS{
                "index.html": &fstest.MapFile{
                    Data: []byte("index"),
                },
            },
        ),
    )

    statusCode, _, bodyReader, served := server.ServeReader(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/static/"),
        logging.NewNopLogger(),
    )

    if false == served {
        t.Fatalf("expected the mount point alone to answer the index file")
    }

    if http.StatusOK != statusCode {
        t.Fatalf("expected 200, got %d", statusCode)
    }

    content, readErr := io.ReadAll(bodyReader)
    if nil != readErr {
        t.Fatalf("read error: %v", readErr)
    }

    _ = bodyReader.Close()

    if "index" != string(content) {
        t.Fatalf("expected the index body, got %q", string(content))
    }
}

func TestFileServer_ServeReader_APathOutsideTheMountIsDeclined(t *testing.T) {
    logger := &levelRecordingLogger{Logger: logging.NewNopLogger()}

    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(
                ModeEmbedded,
                "",
                "index.html",
                "/static/",
                false,
                0,
                false,
            ),
            "",
            fstest.MapFS{
                "a.txt": &fstest.MapFile{
                    Data: []byte("a"),
                },
            },
        ),
    )

    _, _, bodyReader, served := server.ServeReader(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/elsewhere/a.txt"),
        logger,
    )

    if true == served {
        if nil != bodyReader {
            _ = bodyReader.Close()
        }

        t.Fatalf("expected a path outside the mount to be declined")
    }

    if 0 != len(logger.warningMessages) {
        t.Fatalf("expected the mount to decline the path before the canonical check refused it, got %v", logger.warningMessages)
    }
}

func TestFileServer_ServeReader_AMountWithoutATrailingSlashDoesNotMatchALongerSegment(t *testing.T) {
    logger := &levelRecordingLogger{Logger: logging.NewNopLogger()}

    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(
                ModeEmbedded,
                "",
                "index.html",
                "/static",
                false,
                0,
                false,
            ),
            "",
            fstest.MapFS{
                "ky/a.txt": &fstest.MapFile{
                    Data: []byte("a"),
                },
            },
        ),
    )

    _, _, bodyReader, served := server.ServeReader(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/staticky/a.txt"),
        logger,
    )

    if true == served {
        if nil != bodyReader {
            _ = bodyReader.Close()
        }

        t.Fatalf("expected a longer segment not to be absorbed by the mount")
    }

    if false == recordedContains(logger.warningMessages, "static serve non canonical path") {
        t.Fatalf("expected the mismatch to be recorded as non canonical, got %v", logger.warningMessages)
    }
}

func TestFileServer_ServeReader_TheEmbeddedPublicDirectoryIsJoinedOntoThePath(t *testing.T) {
    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(
                ModeEmbedded,
                "public",
                "index.html",
                "",
                false,
                0,
                false,
            ),
            "",
            fstest.MapFS{
                "public/a.txt": &fstest.MapFile{
                    Data: []byte("a"),
                },
            },
        ),
    )

    statusCode, _, bodyReader, served := server.ServeReader(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/a.txt"),
        logging.NewNopLogger(),
    )

    if false == served {
        t.Fatalf("expected the embedded public directory to be joined onto the path")
    }

    if http.StatusOK != statusCode {
        t.Fatalf("expected 200, got %d", statusCode)
    }

    content, readErr := io.ReadAll(bodyReader)
    if nil != readErr {
        t.Fatalf("read error: %v", readErr)
    }

    _ = bodyReader.Close()

    if "a" != string(content) {
        t.Fatalf("expected the body out of the embedded public directory, got %q", string(content))
    }
}

func TestFileServer_ServeReader_ACachedAnswerCarriesTheTagTheDateAndTheCacheControl(t *testing.T) {
    modifiedAt := time.Date(2026, 1, 3, 12, 34, 56, 0, time.UTC)

    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(
                ModeEmbedded,
                "",
                "index.html",
                "",
                true,
                600,
                false,
            ),
            "",
            fstest.MapFS{
                "a.txt": &fstest.MapFile{
                    Data:    []byte("a"),
                    ModTime: modifiedAt,
                },
            },
        ),
    )

    statusCode, headers, file, _, served := server.serveForStreaming(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/a.txt"),
        logging.NewNopLogger(),
    )

    if false == served {
        t.Fatalf("expected the file to resolve")
    }

    if nil != file {
        _ = file.Close()
    }

    if http.StatusOK != statusCode {
        t.Fatalf("expected 200, got %d", statusCode)
    }

    expectedEtag := GenerateEtag(&staticEtagFileInfo{size: 1, modTime: modifiedAt}, false)
    if expectedEtag != headers.Get("ETag") {
        t.Fatalf("expected the entity tag %q, got %q", expectedEtag, headers.Get("ETag"))
    }

    if modifiedAt.Format(http.TimeFormat) != headers.Get("Last-Modified") {
        t.Fatalf("expected the modification date, got %q", headers.Get("Last-Modified"))
    }

    if "public, max-age=600" != headers.Get("Cache-Control") {
        t.Fatalf("expected the configured cache control, got %q", headers.Get("Cache-Control"))
    }
}

func TestFileServer_ServeReader_AMatchingTagAnswersNotModifiedAndClosesTheFile(t *testing.T) {
    modifiedAt := time.Date(2026, 1, 3, 12, 34, 56, 0, time.UTC)

    fileSystem := &trackingFileSystem{
        inner: fstest.MapFS{
            "a.txt": &fstest.MapFile{
                Data:    []byte("a"),
                ModTime: modifiedAt,
            },
        },
    }

    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(
                ModeEmbedded,
                "",
                "index.html",
                "",
                true,
                600,
                false,
            ),
            "",
            fileSystem,
        ),
    )

    request := testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/a.txt")
    request.HttpRequest().Header.Set("If-None-Match", GenerateEtag(&staticEtagFileInfo{size: 1, modTime: modifiedAt}, false))

    statusCode, _, file, _, served := server.serveForStreaming(request, logging.NewNopLogger())

    if false == served {
        t.Fatalf("expected the conditional request to be answered")
    }

    if http.StatusNotModified != statusCode {
        t.Fatalf("expected 304, got %d", statusCode)
    }

    if nil != file {
        _ = file.Close()

        t.Fatalf("expected a 304 to hand back no file")
    }

    if 1 != fileSystem.closedCount {
        t.Fatalf("expected the 304 to close the file it opened, closed %d", fileSystem.closedCount)
    }
}

func TestFileServer_ServeReader_AnUnchangedDateAnswersNotModifiedAndClosesTheFile(t *testing.T) {
    modifiedAt := time.Date(2026, 1, 3, 12, 34, 56, 0, time.UTC)

    fileSystem := &trackingFileSystem{
        inner: fstest.MapFS{
            "a.txt": &fstest.MapFile{
                Data:    []byte("a"),
                ModTime: modifiedAt,
            },
        },
    }

    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(
                ModeEmbedded,
                "",
                "index.html",
                "",
                true,
                600,
                false,
            ),
            "",
            fileSystem,
        ),
    )

    request := testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/a.txt")
    request.HttpRequest().Header.Set("If-Modified-Since", modifiedAt.Format(http.TimeFormat))

    statusCode, _, file, _, served := server.serveForStreaming(request, logging.NewNopLogger())

    if false == served {
        t.Fatalf("expected the conditional request to be answered")
    }

    if http.StatusNotModified != statusCode {
        t.Fatalf("expected 304, got %d", statusCode)
    }

    if nil != file {
        _ = file.Close()

        t.Fatalf("expected a 304 to hand back no file")
    }

    if 1 != fileSystem.closedCount {
        t.Fatalf("expected the 304 to close the file it opened, closed %d", fileSystem.closedCount)
    }
}

func TestFileServer_ServeReader_TheDateIsIgnoredWhenATagWasOffered(t *testing.T) {
    modifiedAt := time.Date(2026, 1, 3, 12, 34, 56, 0, time.UTC)

    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(
                ModeEmbedded,
                "",
                "index.html",
                "",
                true,
                600,
                false,
            ),
            "",
            fstest.MapFS{
                "a.txt": &fstest.MapFile{
                    Data:    []byte("a"),
                    ModTime: modifiedAt,
                },
            },
        ),
    )

    request := testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/a.txt")
    request.HttpRequest().Header.Set("If-None-Match", "\"an-older-deployment\"")
    request.HttpRequest().Header.Set("If-Modified-Since", modifiedAt.Format(http.TimeFormat))

    statusCode, _, file, _, served := server.serveForStreaming(request, logging.NewNopLogger())

    if false == served {
        t.Fatalf("expected the request to be answered")
    }

    if nil != file {
        _ = file.Close()
    }

    if http.StatusOK != statusCode {
        t.Fatalf("expected the whole body for a tag that does not match, got %d", statusCode)
    }
}

func TestFileServer_TheBufferedAndStreamingResolutionsAgreeOnEveryRefusal(t *testing.T) {
    fileSystem := fstest.MapFS{
        "a.txt": &fstest.MapFile{
            Data: []byte("a"),
        },
        "internal/secret.json": &fstest.MapFile{
            Data: []byte("secret"),
        },
        ".env": &fstest.MapFile{
            Data: []byte("APP_SECRET=1"),
        },
        "excluded/a.txt": &fstest.MapFile{
            Data: []byte("a"),
        },
        "assets/a.txt": &fstest.MapFile{
            Data: []byte("a"),
        },
    }

    config := NewFileServerConfig(
        ModeEmbedded,
        "",
        "index.html",
        "",
        false,
        0,
        false,
    )
    config.SetExcludedPathList([]string{"/excluded/"})

    server := NewFileServer(NewOptions(config, "", fileSystem))

    for _, probe := range []struct {
        name        string
        method      string
        requestPath string
        served      bool
    }{
        {name: "an ordinary asset", method: http.MethodGet, requestPath: "/a.txt", served: true},
        {name: "a path that is not canonical", method: http.MethodGet, requestPath: "/open/../internal/secret.json", served: false},
        {name: "a dot prefixed element", method: http.MethodGet, requestPath: "/.env", served: false},
        {name: "an excluded prefix", method: http.MethodGet, requestPath: "/excluded/a.txt", served: false},
        {name: "a method that is not a retrieval", method: http.MethodPost, requestPath: "/a.txt", served: false},
        {name: "a directory", method: http.MethodGet, requestPath: "/assets", served: false},
        {name: "a name that is not there", method: http.MethodGet, requestPath: "/absent.txt", served: false},
    } {
        _, _, _, bufferedServed := server.Serve(
            testhelper.NewHttpTestRequest(probe.method, "http://example.com"+probe.requestPath),
            logging.NewNopLogger(),
        )

        _, _, file, _, streamingServed := server.serveForStreaming(
            testhelper.NewHttpTestRequest(probe.method, "http://example.com"+probe.requestPath),
            logging.NewNopLogger(),
        )

        if nil != file {
            _ = file.Close()
        }

        if probe.served != bufferedServed {
            t.Fatalf("expected the buffered resolution to answer served=%v for %s, got %v", probe.served, probe.name, bufferedServed)
        }

        if bufferedServed != streamingServed {
            t.Fatalf("the two resolutions disagree on %s: buffered=%v streaming=%v", probe.name, bufferedServed, streamingServed)
        }
    }
}

func recordedContains(messages []string, expected string) bool {
    for _, message := range messages {
        if true == strings.Contains(message, expected) {
            return true
        }
    }

    return false
}

func TestNewFileServer_CopiesTheConfigurationAndLeavesTheCallersUntouched(t *testing.T) {
    fileSystem := fstest.MapFS{
        "a.txt": &fstest.MapFile{
            Data: []byte("a"),
        },
    }

    config := NewFileServerConfig(
        ModeEmbedded,
        "",
        "",
        "",
        true,
        -1,
        false,
    )

    server := NewFileServer(
        NewOptions(
            config,
            "",
            fileSystem,
        ),
    )

    if "" != config.indexFile {
        t.Fatalf("expected the caller's config to keep its empty index file, got %q", config.indexFile)
    }

    if -1 != config.cacheMaxAge {
        t.Fatalf("expected the caller's config to keep its negative cache max age, got %d", config.cacheMaxAge)
    }

    _, headers, _, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/a.txt"),
        logging.NewNopLogger(),
    )

    if false == served {
        t.Fatalf("expected the file to be served")
    }

    if false == strings.Contains(headers.Get("Cache-Control"), "max-age=3600") {
        t.Fatalf("expected the default cache max age to land on the server's own copy, got %q", headers.Get("Cache-Control"))
    }
}

func TestFileServer_SetterAfterConstructionDoesNotReachTheBuiltServer(t *testing.T) {
    fileSystem := fstest.MapFS{
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
            fileSystem,
        ),
    )

    config.SetExcludedPathList([]string{"/a.txt"})

    _, _, _, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/a.txt"),
        logging.NewNopLogger(),
    )

    if false == served {
        t.Fatalf("expected the built server to keep serving under its construction-time configuration")
    }
}

func (instance *levelRecordingLogger) Info(message string, context exceptioncontract.Context) {
    instance.infoMessages = append(instance.infoMessages, message)
}

func TestFileServer_ANonRetrievalMethodIsRecordedAtDebugOnTheStreamingHalf(t *testing.T) {
    logger := &levelRecordingLogger{Logger: logging.NewNopLogger()}

    server := newRefusingFileServer(fs.ErrNotExist)

    _, _, _, _, served := server.serveForStreaming(
        testhelper.NewHttpTestRequest(http.MethodPost, "http://example.com/app.css"),
        logger,
    )

    if true == served {
        t.Fatal("expected the non-retrieval method not to be served")
    }

    if 0 != len(logger.infoMessages) {
        t.Fatalf("expected no info record for the ordinary exit, got %v", logger.infoMessages)
    }

    if 1 != len(logger.debugMessages) || "static serve method not eligible" != logger.debugMessages[0] {
        t.Fatalf("expected the debug record naming the exit, got %v", logger.debugMessages)
    }
}

func TestFileServer_ANonRetrievalMethodIsRecordedAtDebugOnTheBufferedHalf(t *testing.T) {
    logger := &levelRecordingLogger{Logger: logging.NewNopLogger()}

    server := newRefusingFileServer(fs.ErrNotExist)

    _, _, _, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodPost, "http://example.com/app.css"),
        logger,
    )

    if true == served {
        t.Fatal("expected the non-retrieval method not to be served")
    }

    if 0 != len(logger.infoMessages) {
        t.Fatalf("expected no info record for the ordinary exit, got %v", logger.infoMessages)
    }

    if 1 != len(logger.debugMessages) || "static serve method not eligible" != logger.debugMessages[0] {
        t.Fatalf("expected the debug record naming the exit, got %v", logger.debugMessages)
    }
}

func TestFileServer_AStripPrefixMismatchIsRecordedAtDebug(t *testing.T) {
    logger := &levelRecordingLogger{Logger: logging.NewNopLogger()}

    server := &FileServer{
        config: NewFileServerConfig(
            ModeFilesystem,
            "/does-not-matter",
            "index.html",
            "/assets",
            false,
            0,
            false,
        ),
        fileSystem: &refusingFileSystem{openErr: fs.ErrNotExist},
    }

    _, _, _, _, served := server.serveForStreaming(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/api/orders"),
        logger,
    )

    if true == served {
        t.Fatal("expected the out-of-prefix path not to be served")
    }

    if 0 != len(logger.infoMessages) {
        t.Fatalf("expected no info record for the ordinary exit, got %v", logger.infoMessages)
    }

    if 1 != len(logger.debugMessages) || "static serve strip prefix mismatch" != logger.debugMessages[0] {
        t.Fatalf("expected the debug record naming the exit, got %v", logger.debugMessages)
    }
}

func TestNewFileServer_RefusesNilOptionsByName(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            NewFileServer(nil)
        },
        "options are required for the static file server",
    )
}

func TestFileServer_AnUndatedFileEmitsNoLastModifiedAndAnswersNoConditional304(t *testing.T) {
    fileSystem := fstest.MapFS{
        "a.txt": &fstest.MapFile{
            Data: []byte("a"),
        },
    }

    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(ModeEmbedded, "", "", "", true, 3600, false),
            "",
            fileSystem,
        ),
    )

    request := testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/a.txt")
    request.HttpRequest().Header.Set("If-Modified-Since", "Mon, 02 Jan 2006 15:04:05 GMT")

    statusCode, headers, body, served := server.Serve(request, logging.NewNopLogger())

    if false == served {
        t.Fatalf("expected the asset to be served")
    }

    if http.StatusOK != statusCode {
        t.Fatalf("expected 200 for a client holding nothing, got %d", statusCode)
    }

    if "a" != string(body) {
        t.Fatalf("expected the body to be served, got %q", body)
    }

    if "" != headers.Get("Last-Modified") {
        t.Fatalf("expected no Last-Modified for an undated file, got %q", headers.Get("Last-Modified"))
    }

    if "" == headers.Get("ETag") {
        t.Fatalf("expected the entity tag to remain the validator for an undated file")
    }
}

func TestFileServer_ServeReader_AnUndatedFileEmitsNoLastModifiedAndAnswersNoConditional304(t *testing.T) {
    fileSystem := fstest.MapFS{
        "a.txt": &fstest.MapFile{
            Data: []byte("a"),
        },
    }

    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(ModeEmbedded, "", "", "", true, 3600, false),
            "",
            fileSystem,
        ),
    )

    request := testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/a.txt")
    request.HttpRequest().Header.Set("If-Modified-Since", "Mon, 02 Jan 2006 15:04:05 GMT")

    statusCode, headers, bodyReader, served := server.ServeReader(request, logging.NewNopLogger())

    if false == served {
        t.Fatalf("expected the asset to be served")
    }

    if nil != bodyReader {
        defer bodyReader.Close()
    }

    if http.StatusOK != statusCode {
        t.Fatalf("expected 200 for a client holding nothing, got %d", statusCode)
    }

    if "" != headers.Get("Last-Modified") {
        t.Fatalf("expected no Last-Modified for an undated file, got %q", headers.Get("Last-Modified"))
    }
}

func TestFileServer_ADatedFileKeepsItsLastModifiedAndItsConditional304(t *testing.T) {
    modifiedAt := time.Date(2026, 1, 3, 12, 34, 56, 0, time.UTC)

    fileSystem := fstest.MapFS{
        "a.txt": &fstest.MapFile{
            Data:    []byte("a"),
            ModTime: modifiedAt,
        },
    }

    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(ModeEmbedded, "", "", "", true, 3600, false),
            "",
            fileSystem,
        ),
    )

    request := testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/a.txt")
    request.HttpRequest().Header.Set("If-Modified-Since", modifiedAt.Format(http.TimeFormat))

    statusCode, headers, _, served := server.Serve(request, logging.NewNopLogger())

    if false == served {
        t.Fatalf("expected the asset to be served")
    }

    if http.StatusNotModified != statusCode {
        t.Fatalf("expected 304 for a dated file the client already holds, got %d", statusCode)
    }

    if "" == headers.Get("Last-Modified") {
        t.Fatalf("expected a dated file to keep its Last-Modified")
    }
}

func TestNewFileServer_RefusesAPublicDirectoryTheEmbeddedFileSystemDoesNotHold(t *testing.T) {
    fileSystem := fstest.MapFS{
        "public/app.css": &fstest.MapFile{
            Data: []byte("body{}"),
        },
    }

    testhelper.AssertPanicsWithError(
        t,
        func() {
            NewFileServer(
                NewOptions(
                    NewFileServerConfig(ModeEmbedded, "web", "", "", false, 0, false),
                    "",
                    fileSystem,
                ),
            )
        },
        "the public directory is not present in the embedded file system",
    )
}

func TestNewFileServer_RefusesAnEmbeddedPublicDirectoryThatIsAFile(t *testing.T) {
    fileSystem := fstest.MapFS{
        "public": &fstest.MapFile{
            Data: []byte("not a directory"),
        },
    }

    testhelper.AssertPanicsWithError(
        t,
        func() {
            NewFileServer(
                NewOptions(
                    NewFileServerConfig(ModeEmbedded, "public", "", "", false, 0, false),
                    "",
                    fileSystem,
                ),
            )
        },
        "the public directory is not present in the embedded file system",
    )
}

func TestNewFileServer_AcceptsAnEmptyEmbeddedPublicDirectory(t *testing.T) {
    fileSystem := fstest.MapFS{
        "app.css": &fstest.MapFile{
            Data: []byte("body{}"),
        },
    }

    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(ModeEmbedded, "", "", "", false, 0, false),
            "",
            fileSystem,
        ),
    )

    _, _, body, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/app.css"),
        logging.NewNopLogger(),
    )

    if false == served {
        t.Fatalf("expected the asset to be served from the root of the embedded file system")
    }

    if "body{}" != string(body) {
        t.Fatalf("unexpected body %q", body)
    }
}

func TestFileServer_RefusesAFifoInThePublicDirectory(t *testing.T) {
    directory := t.TempDir()

    fifoPath := directory + "/pipe.html"
    mkfifoErr := syscall.Mkfifo(fifoPath, 0o644)
    if nil != mkfifoErr {
        t.Fatalf("mkfifo error: %v", mkfifoErr)
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

    done := make(chan bool, 1)
    go func() {
        _, _, _, served := server.Serve(
            testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/pipe.html"),
            logging.NewNopLogger(),
        )
        done <- served
    }()

    select {
    case served := <-done:
        if true == served {
            t.Fatalf("expected the fifo not to be served")
        }
    case <-time.After(5 * time.Second):
        t.Fatalf("the request goroutine parked inside the open — the mode check did not run before it")
    }
}

func TestFileServer_ATypedNilRequestIsSkippedByBothDoors(t *testing.T) {
    directory := t.TempDir()

    if err := osWriteFile(directory+"/index.html", []byte("hello")); nil != err {
        t.Fatalf("write file error: %v", err)
    }

    server := NewFileServer(
        NewOptions(
            NewFileServerConfig(ModeFilesystem, directory, "index.html", "", false, 0, false),
            "",
            nil,
        ),
    )

    var unassignedRequest *testhelper.HttpTestRequest

    serveLogger := &levelRecordingLogger{Logger: logging.NewNopLogger()}
    if _, _, _, served := server.Serve(unassignedRequest, serveLogger); true == served {
        t.Fatalf("expected Serve to skip a typed nil request")
    }
    if 1 != len(serveLogger.warningMessages) || "static serve skipped because request is nil" != serveLogger.warningMessages[0] {
        t.Fatalf("expected Serve's own refusal to be the one that answered, got %v", serveLogger.warningMessages)
    }

    readerLogger := &levelRecordingLogger{Logger: logging.NewNopLogger()}
    if _, _, _, served := server.ServeReader(unassignedRequest, readerLogger); true == served {
        t.Fatalf("expected ServeReader to skip a typed nil request")
    }
    if 1 != len(readerLogger.warningMessages) || "static serve reader skipped because request is nil" != readerLogger.warningMessages[0] {
        t.Fatalf("expected ServeReader's own refusal to be the one that answered, got %v", readerLogger.warningMessages)
    }
}

func TestFileServer_Embedded_RefusesAnEntryThatIsNotARegularFile(t *testing.T) {
    fileSystem := fstest.MapFS{
        "pipe.txt": &fstest.MapFile{
            Data: []byte("would be served"),
            Mode: fs.ModeNamedPipe,
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

    server := NewFileServer(NewOptions(config, "", fileSystem))
    logger := &levelRecordingLogger{}

    statusCode, _, body, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/pipe.txt"),
        logger,
    )

    if true == served {
        t.Fatalf("expected a named pipe to be refused, got status %d and %d bytes", statusCode, len(body))
    }

    found := false
    for _, message := range logger.infoMessages {
        if "static serve target is not a regular file" == message {
            found = true
        }
    }

    if false == found {
        t.Fatalf("expected the mode refusal to be recorded, got info %v", logger.infoMessages)
    }
}

func TestFileServer_ExclusionListReadsTheSpellingTheRouterRoutes(t *testing.T) {
    fileSystem := fstest.MapFS{
        "assets%2Fx.txt": &fstest.MapFile{Data: []byte("ONE SEGMENT")},
        "assets/y.txt":   &fstest.MapFile{Data: []byte("UNDER THE DIRECTORY")},
    }

    config := NewFileServerConfig(
        ModeEmbedded,
        "",
        "index.html",
        "/static/",
        false,
        0,
        false,
    )

    config.SetExcludedPathList([]string{"/static/assets/"})

    server := NewFileServer(
        NewOptions(
            config,
            "",
            fileSystem,
        ),
    )

    statusCode, _, body, served := server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/static/assets%2Fx.txt"),
        logging.NewNopLogger(),
    )

    if false == served || http.StatusOK != statusCode || "ONE SEGMENT" != string(body) {
        t.Fatalf("expected the one-segment resource outside the excluded directory to be served, got served=%v status=%d body=%q", served, statusCode, string(body))
    }

    _, _, _, served = server.Serve(
        testhelper.NewHttpTestRequest(http.MethodGet, "http://example.com/static/assets/y.txt"),
        logging.NewNopLogger(),
    )

    if true == served {
        t.Fatal("expected the file under the excluded directory to be declined")
    }
}
