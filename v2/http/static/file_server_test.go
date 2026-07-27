package static

import (
    "bytes"
    "io/fs"
    "net/http"
    "os"
    "strings"
    "testing"
    "testing/fstest"
    "time"

    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    "github.com/precision-soft/melody/v2/internal/testhelper"
    "github.com/precision-soft/melody/v2/logging"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
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

/* the root, and every spelling that folds into it, answers the configured index file: that resolution is named by configuration and is what a browser asks for by visiting the site */
func TestFileServer_ServesTheIndexFileForTheSpellingsThatFoldIntoTheRoot(t *testing.T) {
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

    server := NewFileServer(
        NewOptions(
            config,
            "",
            fileSystem,
        ),
    )

    for _, requestPath := range []string{
        "http://example.com/",
        "http://example.com/.",
        "http://example.com/..",
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

    _, _, _, servedAfterClearing := server.Serve(
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

/* @info A path whose symlinks resolve outside the served directory comes back from resolveAndOpen as fs.ErrPermission, and it is the only thing here that does. Recorded at debug it was byte-identical to a mistyped stylesheet href — the very indistinguishability the logging on this path exists to end — so the level is what carries the distinction and is what this pins. */
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

/* @info the control: an ordinary miss stays at debug. The static server is consulted for every request no route answered, so warning on a miss would file one record per non-asset request and teach an operator to filter the message out — taking the refusal above with it. */
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
