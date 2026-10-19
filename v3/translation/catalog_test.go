package translation

import (
    "testing"
)

/* the empty domain is the caller saying "the ordinary one", not a domain of its own: Add and Get must fold it to the same default, or a message added without a domain would be unreachable through a read that also omits it */
func TestMapCatalog_TheEmptyDomainFoldsToTheDefaultOnBothSides(t *testing.T) {
    catalog := NewMapCatalog("en")

    catalog.Add("", "greeting", "hello")

    if message, found := catalog.Get("greeting", ""); false == found || "hello" != message {
        t.Fatalf("expected the empty domain to read back, got %q found=%v", message, found)
    }

    if message, found := catalog.Get("greeting", DefaultDomain); false == found || "hello" != message {
        t.Fatalf("expected the empty domain to be the default domain, got %q found=%v", message, found)
    }

    catalog.Add(DefaultDomain, "farewell", "bye")

    if message, found := catalog.Get("farewell", ""); false == found || "bye" != message {
        t.Fatalf("expected the default domain to read back through the empty one, got %q found=%v", message, found)
    }
}

func TestMapCatalog_KeepsDomainsApart(t *testing.T) {
    catalog := NewMapCatalog("en")

    catalog.Add("messages", "label", "Name").Add("validation", "label", "This field is required")

    if message, _ := catalog.Get("label", "messages"); "Name" != message {
        t.Fatalf("expected the messages domain, got %q", message)
    }

    if message, _ := catalog.Get("label", "validation"); "This field is required" != message {
        t.Fatalf("expected the validation domain, got %q", message)
    }
}

/* the reader reports absence rather than an empty string: an empty translation is a legitimate value, and a caller that cannot tell the two apart falls back to the message id for a message that was deliberately blank */
func TestMapCatalog_GetSeparatesAnAbsentMessageFromAnEmptyOne(t *testing.T) {
    catalog := NewMapCatalog("en")

    if message, found := catalog.Get("greeting", "messages"); true == found || "" != message {
        t.Fatalf("expected an unknown domain to answer absent, got %q found=%v", message, found)
    }

    catalog.Add("messages", "other", "value")

    if message, found := catalog.Get("greeting", "messages"); true == found || "" != message {
        t.Fatalf("expected an unknown message id to answer absent, got %q found=%v", message, found)
    }

    catalog.Add("messages", "blank", "")

    if message, found := catalog.Get("blank", "messages"); false == found || "" != message {
        t.Fatalf("expected a deliberately empty message to be found, got %q found=%v", message, found)
    }
}

func TestMapCatalog_ReportsItsLocaleAndChainsAdd(t *testing.T) {
    catalog := NewMapCatalog("ro")

    if "ro" != catalog.Locale() {
        t.Fatalf("expected the locale it was built for, got %q", catalog.Locale())
    }

    if catalog != catalog.Add("messages", "greeting", "salut") {
        t.Fatal("expected Add to answer the catalog it was called on")
    }

    catalog.Add("messages", "greeting", "bună")

    if message, _ := catalog.Get("greeting", "messages"); "bună" != message {
        t.Fatalf("expected the last message added to win, got %q", message)
    }
}
