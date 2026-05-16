package cron

import (
    "testing"
)

func TestSanitizeLogFileNameReplacesColons(t *testing.T) {
    result := sanitizeLogFileName("access-token:cleanup")

    if "access-token-cleanup" != result {
        t.Fatalf("sanitizeLogFileName = %q, want %q", result, "access-token-cleanup")
    }
}

func TestSanitizeLogFileNameReplacesSlashes(t *testing.T) {
    result := sanitizeLogFileName("foo/bar")

    if "foo-bar" != result {
        t.Fatalf("sanitizeLogFileName = %q, want %q", result, "foo-bar")
    }
}

func TestSanitizeLogFileNameKeepsRegularChars(t *testing.T) {
    result := sanitizeLogFileName("regular_command-name")

    if "regular_command-name" != result {
        t.Fatalf("sanitizeLogFileName = %q, want %q", result, "regular_command-name")
    }
}

func TestRawLogFileNameReplacesSlashesButKeepsColons(t *testing.T) {
    result := rawLogFileName("access:token/cleanup")

    if "access:token-cleanup" != result {
        t.Fatalf("rawLogFileName = %q, want %q", result, "access:token-cleanup")
    }
}

func TestSplitLogFileExtensionPlain(t *testing.T) {
    base, extension := splitLogFileExtension("name.log")

    if "name" != base || ".log" != extension {
        t.Fatalf("splitLogFileExtension(\"name.log\") = (%q, %q), want (\"name\", \".log\")", base, extension)
    }
}

func TestSplitLogFileExtensionComposite(t *testing.T) {
    base, extension := splitLogFileExtension("archive.tar.gz")

    if "archive" != base || ".tar.gz" != extension {
        t.Fatalf("splitLogFileExtension(\"archive.tar.gz\") = (%q, %q), want (\"archive\", \".tar.gz\")", base, extension)
    }
}

func TestSplitLogFileExtensionNoExtension(t *testing.T) {
    base, extension := splitLogFileExtension("name")

    if "name" != base || "" != extension {
        t.Fatalf("splitLogFileExtension(\"name\") = (%q, %q), want (\"name\", \"\")", base, extension)
    }
}

func TestSplitLogFileExtensionLeadingDots(t *testing.T) {
    base, extension := splitLogFileExtension(".hidden")

    if ".hidden" != base || "" != extension {
        t.Fatalf("splitLogFileExtension(\".hidden\") = (%q, %q), want (\".hidden\", \"\")", base, extension)
    }
}

func TestSplitLogFileExtensionAllDots(t *testing.T) {
    base, extension := splitLogFileExtension("...")

    if "..." != base || "" != extension {
        t.Fatalf("splitLogFileExtension(\"...\") = (%q, %q), want (\"...\", \"\")", base, extension)
    }
}

func TestSplitLogFileExtensionLeadingDotsThenExt(t *testing.T) {
    base, extension := splitLogFileExtension("..a.log")

    if "..a" != base || ".log" != extension {
        t.Fatalf("splitLogFileExtension(\"..a.log\") = (%q, %q), want (\"..a\", \".log\")", base, extension)
    }
}

func TestSplitLogFileExtensionHiddenWithExtension(t *testing.T) {
    base, extension := splitLogFileExtension(".hidden.log")

    if ".hidden" != base || ".log" != extension {
        t.Fatalf("splitLogFileExtension(\".hidden.log\") = (%q, %q), want (\".hidden\", \".log\")", base, extension)
    }
}

func TestSplitLogFileExtensionEmpty(t *testing.T) {
    base, extension := splitLogFileExtension("")

    if "" != base || "" != extension {
        t.Fatalf("splitLogFileExtension(\"\") = (%q, %q), want (\"\", \"\")", base, extension)
    }
}
