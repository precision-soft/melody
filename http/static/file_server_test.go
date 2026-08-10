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
    "testing"
    "testing/fstest"
    "time"

    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal/testhelper"
    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
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

/* the strip prefix leaves a relative remainder, so a leading ".." survives path.Clean and the join with the public directory absorbs it; the request must stay confined to the public directory instead of reaching a sibling of it in the embedded filesystem */
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

/* the access-control matchers in front of the application compare the raw request path, so "/ internal/secret.json" never matches a rule written for "/internal/"; resolving the trimmed spelling would hand out the very file the rule protects, and it would do so only in the filesystem mode, since the embedded tree has no entry under the padded name */
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

/* the spelling that arrives is the only one the rules in front of the application judge, so a file may only be answered under the path it actually sits at: "/open/../internal/secret.json" is not matched by a rule on "/internal/" and must not reach the file either */
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

/* the strip prefix is configuration, so the spelling has to be judged against the whole path: the doubled slash of "/static//a.txt" is swallowed by the strip and would leave a canonical-looking remainder behind */
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

/* The mount root answers the configured index file — that resolution is named by configuration and is what
a browser asks for by visiting the site — but only for the root as it is actually spelled. The branch that
resolves every other path refuses a spelling `path.Clean` folded, because the matchers in front of the
application compare the raw path; the root branch carries the same refusal, or the mount's index page is
served from behind whatever rule the folded-away prefix carried. */
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

/* The two halves are textual twins and only the streaming one has a production caller, so the refusal is
asserted on it by name rather than through Serve alone. */
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

/* the embed directive spells "all:public", so the dotfiles a deployment keeps beside its assets travel into the binary; answering one also labels it publicly cacheable under the shipped cache configuration, which puts a copy in every shared cache on the way back */
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

/* a method other than a retrieval belongs to whatever the application routes the path to: answering a DELETE with the file body reports a deletion that never happened, and answering an OPTIONS preflight with a body sends none of the Access-Control-Allow-* headers the browser asked for, which the browser reads as a refusal */
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

/* the entity tag is the accurate validator and the client already offered one, so the modification date carries no vote: a deploy that rewrites content while preserving modification times would otherwise answer 304 to a cache that just proved it holds different bytes */
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

/* an HTTP date may arrive in any of three formats and only one of them is the fixdate; parsing that one alone re-sends the whole body to every client whose cache writes asctime or the RFC 850 form */
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

/* every static byte a running application serves is resolved through the streaming path, so a refusal that logs nothing there leaves a traversal attempt, a symlink escape and an unreadable path with no trace at all */
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
    /* the exclusion names the url an application reasons about, which is the whole path a client sends; comparing what is left after the mount prefix is trimmed would make one entry mean a different url per mount. */
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

/* refusingFileSystem answers every open with one chosen error, which is how the two shapes of open failure are told apart without a symlink the test would have to build on a filesystem that may not support one. */
type refusingFileSystem struct {
    openErr error
}

func (instance *refusingFileSystem) Open(name string) (fs.File, error) {
    return nil, instance.openErr
}

/* levelRecordingLogger keeps what was written and at which level, since the level is the whole assertion here. */
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

/* A path whose symlinks resolve outside the served directory comes back from resolveAndOpen as fs.ErrPermission, and it is the only thing here that does. Recorded at debug it was byte-identical to a mistyped stylesheet href — the very indistinguishability the logging on this path exists to end — so the level is what carries the distinction and is what this pins. */
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

/* the control: an ordinary miss stays at debug. The static server is consulted for every request no route answered, so warning on a miss would file one record per non-asset request and teach an operator to filter the message out — taking the refusal above with it. */
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

/* the exclusion list is consulted with the spelling the client sent, and the root resolves to the index file only afterwards: an exclusion naming the index file must fire for "/" too, or the one URL the operator handed to the application is answered off the disk by the outermost middleware */

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

/* trackingFileSystem hands out files that record their own closing. Wherever the streaming resolution refuses after it has already opened a file — a stat that fails, a target that turns out to be a directory, a conditional request answered 304 — the refusal has to close what it opened, or a running application leaks one descriptor for every such request; the count is that assertion. */
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

/* statFailingFileSystem opens successfully and then refuses to describe what it opened, which is the shape that reaches the stat refusal: a file unlinked between the open and the description, or a mount that answers an open out of a cache it can no longer stat. */
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

/* readFailingFileSystem describes an ordinary file and then fails to hand over its bytes — a truncated network mount, a medium error — which is the only door to the buffered resolution's read failure. The streaming resolution has no such door: it hands the open file to the caller without reading it. */
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

/* the public directory is what a deployment names in its configuration, and leaving it unset is the ordinary case rather than a mistake: the shipped default is "public", so an application that says nothing serves the directory the convention names. */
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

/* a relative public directory is anchored on the configured root, and an unset root means the directory the process was started in — which is what a development run and a container whose working directory is the application both rely on. The base path is asserted rather than a served file, because the anchoring is the whole behaviour and a served file would only prove it for whatever directory the test happens to run in. */
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

/* the embedded mode carries no directory of its own, so the filesystem is the one thing the caller must hand over; without the refusal the server is built and every request panics on the nil filesystem, one per request, in the outermost middleware. */
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

/* the streaming door is the one every request takes, so a nil request has to be answered there rather than panicking inside the resolution. The message is its own — the buffered door says "static serve skipped", this one says "static serve reader skipped" — so the record names which door was knocked on. */
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

/* the buffered door records its own refusal under its own message, and the two are distinguishable on purpose: an application that calls Serve directly and one that goes through the middleware leave different records for the same mistake. */
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

/* the resolution has a nil check of its own, below the door's. It is reached only white-box, because the door refuses first, and it is the one refusal in the whole resolution that records nothing — deliberately, since the door above it has already said so. */
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

/* a HEAD answers the size without the bytes, and it has to close the file it opened to learn that size: the resolution hands back an open file for every successful retrieval, and the door is what decides no body will be read. Without the close a running application leaks one descriptor per HEAD, which is what a health check and a link checker send. */
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

/* a 304 travels back through the door with no body at all, and the file was already closed by the resolution that decided it — so the door must hand the status through rather than fall into the body path, where the nil file it was given would be read. */
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

/* both of the door's nil-header guards are LATENT, and this is the measurement: the resolution builds its header map before any answer is decided and hands the same map back on every outcome it reports as resolved — the 200 and the 304 alike. Neither guard can fire while that holds, and if it stops holding this test is what says so. */
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

/* the door's refusal of a file that is not a read closer is LATENT, and this is the measurement rather than a reading: the fs.File contract carries Read and Close, so every type that satisfies it satisfies io.ReadCloser too and the assertion cannot fail for a value that exists. The declaration below is the proof — it is a compile error the day fs.File stops carrying either method, which is the day the refusal becomes reachable. */
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

/* the spellings that fold onto the mount root all clean to "/", never to "." and never to the empty string, because the received path is anchored with a leading slash before it is cleaned. The two guards that answer "." and "" are therefore LATENT, and this is the measurement of why — a defence against a folding rule that does not hold today. */
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

/* the mount root resolves to the configured index file without cleaning it again, so an index file naming an escape reaches the filesystem as a relative path that leaves the served directory. It is refused there, which is the one place left to refuse it: the name came from configuration, not from the request, so no earlier guard was ever aimed at it. */
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

/* a file that opens and then refuses to describe itself is not served. The buffered resolution closes it through its deferred close; the streaming one has no such deferral and closes it by hand, which is the difference the twin below pins. */
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

/* a directory is not an asset. Answering one with its bytes hands out whatever the filesystem calls a directory read, and answering it with a listing publishes the names of everything beside it — so it is refused, and the streaming twin closes what it opened while refusing. */
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

/* a read that fails after the file was described is the one failure the buffered resolution answers with a status instead of a refusal: the headers are already decided and the request was already claimed, so declining here would run the rest of the chain for a request the file server had taken. The streaming resolution never reaches this — it hands the open file over unread. */
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

/* the strip prefix is what mounts the file server under part of the url, and the streaming resolution is the one every request takes — so the matching half, the trimming, and the refusal of a path outside the mount all belong here rather than only on the buffered twin. */
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

/* the mount point itself is the site root as far as the client is concerned, so a request for the prefix alone answers the index file.

The substitution that puts "/" back is SHADOWED, and this test is not its proof: the anchoring below it prefixes a leading slash onto whatever the trim left, so an empty remainder folds onto the root either way and removing the substitution changes nothing observable. It is proved on its verdict instead, by the inversion that turns every non-empty remainder into the root — which is the mount serving its index file for every asset beneath it. */
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

/* a path outside the mount belongs to whatever the application routed it to, and the file server declines it without looking at the disk. The refusal is SHADOWED on its verdict — a path the mount does not claim also fails the canonical rebuild below, which refuses it too — so the assertion is the record rather than the verdict: the mount declines at info level and says "strip prefix mismatch", while the canonical check refuses at warning level. An empty warning list is therefore what says the mount declined it, and it is what fails when the mount stops declining. */
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

/* a mount written without a trailing slash still matches only whole segments. "/staticky/a.txt" begins with "/static", so the remainder is "ky/a.txt" — a name with no leading slash, which is anchored before it is cleaned and then fails to match the path that arrived. Rebuilding the path around the mount is what catches it: comparing only the remainder would let the segment boundary be absorbed. */
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

/* the embedded mode packs the public directory into the binary under its own name, so the resolved path is joined onto it before the filesystem is asked. Without the join the request reaches for the file at the root of the embedded tree, where it is not. */
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

/* every caching header a static answer carries is decided on the streaming resolution, which is the one a running application uses — and none of it had a test there. The tag is what a revalidating cache offers back, the modification date is what an older one offers, and the cache control is what tells a shared cache it may keep the copy at all. */
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

/* the tag a revalidating cache offers back is answered on the streaming resolution too, and the answer closes the file it had already opened to build the tag. */
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

/* the modification date is the older cache's question, and the streaming resolution answers it as well — closing the file it opened, exactly as the tag answer does. */
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

/* a client that offered a tag has already said which bytes it holds, so the date is not consulted at all — otherwise a deploy that rewrites content while preserving timestamps answers 304 to a cache that just proved, by offering a tag that does not match, that it holds different bytes. The buffered twin has this test; the streaming one, which is the path every request takes, did not. */
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

/* the buffered and the streaming resolutions carry the same refusals, written twice. Only the streaming one is ever reached by a request, so a guard repaired on one and forgotten on the other would leave the reachable half open with the battery green. This is not the proof of any single guard — each has its own test, where it is the only candidate — it is the signal that the two halves have not drifted apart. */
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

/* the non-retrieval method is ordinary control flow for a globally mounted middleware — every POST in the application takes this exit — and is recorded at debug, the level logOpenFailure's own comment reserves for the per-request ordinary case; at info it doubled the journal of every api request. Both textual halves answer alike. */
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

/* every request outside the mounted prefix takes the mismatch exit, so it is recorded at debug for the reason the ordinary miss is. */
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

/* nil options are refused by name: the default here would be a live file server over "public", and a wiring mistake must not start serving files nobody asked served. */
func TestNewFileServer_RefusesNilOptionsByName(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            NewFileServer(nil)
        },
        "options are required for the static file server",
    )
}
