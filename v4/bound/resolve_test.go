package bound

import (
    "errors"
    "go/ast"
    "go/parser"
    "go/token"
    "io/fs"
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/exception"
)

func timeoutRule(unlimitedAdmitted bool) Rule[time.Duration] {
    return Rule[time.Duration]{
        Name:              "Timeout",
        Default:           30 * time.Second,
        Minimum:           TimeoutMinimum,
        UnlimitedAdmitted: unlimitedAdmitted,
    }
}

func TestResolve_AbsentTakesTheDefault(t *testing.T) {
    resolved, err := Resolve(Duration{}, timeoutRule(false))
    if nil != err || 30*time.Second != resolved.Value || true == resolved.Unlimited {
        t.Fatalf("expected the 30s default, got %+v (%v)", resolved, err)
    }
}

func TestResolve_AValueAtTheMinimumIsAdmitted(t *testing.T) {
    resolved, err := Resolve(Of(time.Millisecond), timeoutRule(false))
    if nil != err || time.Millisecond != resolved.Value {
        t.Fatalf("expected 1ms, got %+v (%v)", resolved, err)
    }
}

func TestResolve_AValueBelowTheMinimumIsRefusedByName(t *testing.T) {
    for _, value := range []time.Duration{0, -time.Second, time.Millisecond - 1} {
        _, err := Resolve(Of(value), timeoutRule(true))
        assertRefusedField(t, err, "Timeout", "the limit is below its minimum")
    }
}

func TestResolve_ZeroIsAValueWhereTheMinimumIsZero(t *testing.T) {
    resolved, err := Resolve(Of(0), Rule[int]{Name: "Retries", Default: 3, Minimum: 0})
    if nil != err || 0 != resolved.Value || true == resolved.Unlimited {
        t.Fatalf("expected the value 0, got %+v (%v)", resolved, err)
    }
}

func TestResolve_UnlimitedFollowsTheRule(t *testing.T) {
    resolved, err := Resolve(UnlimitedDuration(), timeoutRule(true))
    if nil != err || false == resolved.Unlimited || 0 != resolved.Value {
        t.Fatalf("expected unlimited, got %+v (%v)", resolved, err)
    }

    _, err = Resolve(UnlimitedDuration(), timeoutRule(false))
    assertRefusedField(t, err, "Timeout", "the limit does not admit unlimited")
}

func TestResolve_AbsentIsUnlimitedUnderDefaultUnlimited(t *testing.T) {
    rule := Rule[int]{Name: "Limit", DefaultUnlimited: true, Minimum: 0}

    resolved, err := Resolve(Count{}, rule)
    if nil != err || false == resolved.Unlimited || 0 != resolved.Value {
        t.Fatalf("expected an absent limit to resolve as unlimited, got %+v (%v)", resolved, err)
    }

    resolved, err = Resolve(UnlimitedCount(), rule)
    if nil != err || false == resolved.Unlimited {
        t.Fatalf("expected an explicit unlimited to be admitted, got %+v (%v)", resolved, err)
    }

    resolved, err = Resolve(Of(0), rule)
    if nil != err || true == resolved.Unlimited || 0 != resolved.Value {
        t.Fatalf("expected 0 to stay a value, got %+v (%v)", resolved, err)
    }

    _, err = Resolve(Of(-1), rule)
    assertRefusedField(t, err, "Limit", "the limit is below its minimum")
}

func TestResolve_AValueAboveTheMaximumIsRefusedByName(t *testing.T) {
    rule := Rule[int]{Name: "Digits", Default: 6, Minimum: 6, Maximum: 8}

    resolved, err := Resolve(Of(8), rule)
    if nil != err || 8 != resolved.Value {
        t.Fatalf("expected the maximum to be admitted, got %+v (%v)", resolved, err)
    }

    _, err = Resolve(Of(9), rule)
    assertRefusedField(t, err, "Digits", "the limit is above its maximum")

    var refusal *exception.Error
    if false == errors.As(err, &refusal) || "8" != refusal.Context()["maximum"] || "9" != refusal.Context()["value"] {
        t.Fatalf("expected the refusal to carry the value and the maximum, got %v", err)
    }
}

func TestResolve_AZeroMaximumSetsNoCeiling(t *testing.T) {
    resolved, err := Resolve(Of(1_000_000_000), Rule[int]{Name: "Entries", Default: 10, Minimum: 1})
    if nil != err || 1_000_000_000 != resolved.Value {
        t.Fatalf("expected no ceiling under a zero maximum, got %+v (%v)", resolved, err)
    }
}

func TestResolved_LimitKeepsTheResolvedState(t *testing.T) {
    unlimited := Resolved[time.Duration]{Unlimited: true}.Limit()
    if false == unlimited.IsUnlimited() {
        t.Fatalf("expected an unlimited resolution to answer unlimited, got %s", unlimited)
    }

    value, isSet := Resolved[time.Duration]{Value: time.Hour}.Limit().Value()
    if false == isSet || time.Hour != value {
        t.Fatalf("expected a resolved value to answer that value, got %s (%v)", value, isSet)
    }

    zero, isSet := Resolved[int]{}.Limit().Value()
    if false == isSet || 0 != zero {
        t.Fatalf("expected a resolved zero to stay the value 0, got %d (%v)", zero, isSet)
    }
}

func assertRefusedField(t *testing.T, err error, field string, message string) {
    t.Helper()

    var refusal *exception.Error
    if false == errors.As(err, &refusal) {
        t.Fatalf("expected a melody error, got %v", err)
    }

    if message != refusal.Message() {
        t.Fatalf("expected %q, got %q", message, refusal.Message())
    }

    if field != refusal.Context()["field"] {
        t.Fatalf("expected the refusal to name %q, got %+v", field, refusal.Context())
    }
}

var ruleWalkRoots = []string{"..", "../../integrations"}

func isBoundRuleLiteral(expression ast.Expr) bool {
    literal, isLiteral := expression.(*ast.CompositeLit)
    if false == isLiteral {
        return false
    }

    index, isIndex := literal.Type.(*ast.IndexExpr)
    if false == isIndex {
        return false
    }

    switch typeExpression := index.X.(type) {
    case *ast.SelectorExpr:
        return "Rule" == typeExpression.Sel.Name
    case *ast.Ident:
        return "Rule" == typeExpression.Name
    }

    return false
}

func TestRule_NoExportedPackageLevelVariableHoldsARuleLiteral(t *testing.T) {
    found := make([]string, 0)
    walked := 0

    for _, root := range ruleWalkRoots {
        if _, statErr := os.Stat(root); nil != statErr {
            continue
        }

        walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
            if nil != err {
                return err
            }

            if true == entry.IsDir() {
                if "testdata" == entry.Name() || "vendor" == entry.Name() || (strings.HasPrefix(entry.Name(), "v") && "v4" != entry.Name() && 2 == len(entry.Name()) && "." != path) {
                    return filepath.SkipDir
                }

                return nil
            }

            if false == strings.HasSuffix(path, ".go") || true == strings.HasSuffix(path, "_test.go") || false == strings.Contains(filepath.ToSlash(path), "/v4/") && ".." != root {
                return nil
            }

            parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
            if nil != parseErr {
                return parseErr
            }

            walked++

            for _, declaration := range parsed.Decls {
                general, isGeneral := declaration.(*ast.GenDecl)
                if false == isGeneral || token.VAR != general.Tok {
                    continue
                }

                for _, specification := range general.Specs {
                    valueSpecification := specification.(*ast.ValueSpec)
                    for index, name := range valueSpecification.Names {
                        if true == name.IsExported() && index < len(valueSpecification.Values) && true == isBoundRuleLiteral(valueSpecification.Values[index]) {
                            found = append(found, path+":"+name.Name)
                        }
                    }
                }
            }

            return nil
        })
        if nil != walkErr {
            t.Fatalf("walk %s: %v", root, walkErr)
        }
    }

    if 0 == walked {
        t.Fatalf("expected the walk to parse the fourth major's sources")
    }

    if 0 != len(found) {
        t.Fatalf("expected every exported rule to be a function answering it, found variables %v", found)
    }
}
