package config

import (
    "testing"
)

/* An empty value answers nil rather than a deny-all service: nil keeps the door unwired, while a Service built over zero origins would refuse every origin — a different statement, reachable from the same misconfigured variable. */
func TestCorsServiceFromOriginsAnswersNilWhenNoOriginSurvives(t *testing.T) {
    for name, value := range map[string]string{
        "an empty value":       "",
        "only whitespace":      "   ",
        "only empty entries":   " , ,",
        "a lone trailing coma": ",",
    } {
        t.Run(name, func(t *testing.T) {
            if nil != corsServiceFromOrigins(value) {
                t.Fatalf("expected no service for %q", value)
            }
        })
    }
}

func TestCorsServiceFromOriginsAllowsEachDeclaredOrigin(t *testing.T) {
    corsService := corsServiceFromOrigins(" https://catalog.example.test , https://admin.example.test ,")
    if nil == corsService {
        t.Fatalf("expected a service for a populated list")
    }

    if false == corsService.OriginAllowed("https://catalog.example.test") {
        t.Fatalf("the first declared origin was refused")
    }

    if false == corsService.OriginAllowed("https://admin.example.test") {
        t.Fatalf("the second declared origin was refused")
    }

    if true == corsService.OriginAllowed("https://elsewhere.example.test") {
        t.Fatalf("an undeclared origin was allowed")
    }
}
