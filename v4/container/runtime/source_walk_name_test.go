package runtime

import (
    "go/ast"
    "go/parser"
    "go/token"
    "strings"
    "testing"
)

func TestSourceWalk_NoEventTagOrAttributeNameIsAPlainString(t *testing.T) {
    nameDoors := map[string]bool{
        "AddListener":          true,
        "InstallFloorListener": true,
        "DeclareEvent":         true,
        "DispatchName":         true,
        "NewEvent":             true,
    }
    attributeDoors := map[string]bool{
        "Set": true,
        "Get": true,
        "Has": true,
    }
    tagFields := map[string]bool{
        "Tags":                  true,
        "TagsToRunAfterFailure": true,
    }

    doors := 0

    for _, source := range fourthMajorSourceList(t) {
        if true == strings.HasSuffix(source, "_test.go") {
            continue
        }

        fileSet := token.NewFileSet()
        file, parseErr := parser.ParseFile(fileSet, source, nil, 0)
        if nil != parseErr {
            t.Fatalf("parse %s: %v", source, parseErr)
        }

        ast.Inspect(file, func(node ast.Node) bool {
            switch typed := node.(type) {
            case *ast.CallExpr:
                name := calleeName(typed.Fun)
                if true == nameDoors[name] && 0 < len(typed.Args) {
                    doors++
                    if true == isStringLiteral(typed.Args[0]) {
                        t.Errorf("%s: %s names its event through a constant, got %s", fileSet.Position(typed.Pos()), name, typed.Args[0].(*ast.BasicLit).Value)
                    }
                }

                if true == attributeDoors[name] && 0 < len(typed.Args) && true == isAttributesCall(typed.Fun) {
                    doors++
                    if true == isStringLiteral(typed.Args[0]) {
                        t.Errorf("%s: Attributes().%s names its key through a constant, got %s", fileSet.Position(typed.Pos()), name, typed.Args[0].(*ast.BasicLit).Value)
                    }
                }
            case *ast.KeyValueExpr:
                key, isIdent := typed.Key.(*ast.Ident)
                if false == isIdent || false == tagFields[key.Name] {
                    return true
                }

                list, isList := typed.Value.(*ast.CompositeLit)
                if false == isList {
                    return true
                }

                doors++
                for _, element := range list.Elts {
                    if true == isStringLiteral(element) {
                        t.Errorf("%s: %s lists its tags through constants, got %s", fileSet.Position(element.Pos()), key.Name, element.(*ast.BasicLit).Value)
                    }
                }
            case *ast.FuncDecl:
                if "SubscribedEvents" != typed.Name.Name || nil == typed.Body {
                    return true
                }

                ast.Inspect(typed.Body, func(inner ast.Node) bool {
                    literal, isLiteral := inner.(*ast.CompositeLit)
                    if false == isLiteral {
                        return true
                    }

                    if _, isMap := literal.Type.(*ast.MapType); false == isMap {
                        return true
                    }

                    for _, element := range literal.Elts {
                        pair, isPair := element.(*ast.KeyValueExpr)
                        if true == isPair && true == isStringLiteral(pair.Key) {
                            t.Errorf("%s: SubscribedEvents names its events through constants, got %s", fileSet.Position(pair.Pos()), pair.Key.(*ast.BasicLit).Value)
                        }
                    }

                    return true
                })
            }

            return true
        })
    }

    if 50 > doors {
        t.Fatalf("expected the fourth major's event, tag and attribute doors, found %d", doors)
    }
}

func isStringLiteral(expression ast.Expr) bool {
    literal, isLiteral := ast.Unparen(expression).(*ast.BasicLit)

    return true == isLiteral && token.STRING == literal.Kind
}

func isAttributesCall(function ast.Expr) bool {
    selector, isSelector := ast.Unparen(function).(*ast.SelectorExpr)
    if false == isSelector {
        return false
    }

    receiver, isCall := selector.X.(*ast.CallExpr)

    return true == isCall && "Attributes" == calleeName(receiver.Fun)
}
