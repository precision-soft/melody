package config

import (
    "testing"
)

func TestDatabaseWiringFromHostsArmsEachConnectionIndependently(t *testing.T) {
    cases := []struct {
        name            string
        catalogHost     string
        journalHost     string
        expectedCatalog bool
        expectedJournal bool
    }{
        {name: "both hosts arm both connections", catalogHost: "mysql", journalHost: "postgres", expectedCatalog: true, expectedJournal: true},
        {name: "a lone catalog host leaves the journal unwired", catalogHost: "mysql", journalHost: "", expectedCatalog: true, expectedJournal: false},
        {name: "a lone journal host leaves the catalog unwired", catalogHost: "", journalHost: "postgres", expectedCatalog: false, expectedJournal: true},
        {name: "no host wires nothing", catalogHost: "", journalHost: "", expectedCatalog: false, expectedJournal: false},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            wiring := databaseWiringFromHosts(testCase.catalogHost, testCase.journalHost)

            if testCase.expectedCatalog != wiring.catalog {
                t.Fatalf("expected catalog=%v, got %v", testCase.expectedCatalog, wiring.catalog)
            }
            if testCase.expectedJournal != wiring.journal {
                t.Fatalf("expected journal=%v, got %v", testCase.expectedJournal, wiring.journal)
            }
        })
    }
}

func TestDialIsInsecureOnlyOnTheExactSpelling(t *testing.T) {
    if false == dialIsInsecure("true") {
        t.Fatal("expected the exact spelling to arm the insecure dial")
    }

    for _, value := range []string{"", "false", "TRUE", "1", "yes", " true"} {
        if true == dialIsInsecure(value) {
            t.Fatalf("expected %q to keep the verified handshake", value)
        }
    }
}

func TestDatabaseServiceNameFollowsTheCatalogSwitchAlone(t *testing.T) {
    moduleInstance := NewExampleModule()

    moduleInstance.databaseWiring = databaseWiring{catalog: false, journal: true}
    if "" != moduleInstance.databaseServiceName() {
        t.Fatal("expected a journal-only wiring to keep the catalog repositories in-memory")
    }

    moduleInstance.databaseWiring = databaseWiring{catalog: true, journal: false}
    if ServiceExampleDatabase != moduleInstance.databaseServiceName() {
        t.Fatal("expected the armed catalog to answer its service name")
    }
}
