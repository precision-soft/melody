package runtime

import (
    "go/ast"
    "go/parser"
    "go/token"
    "strconv"
    "strings"
    "testing"
)

func TestSourceWalk_EveryNilRuntimeRefusalNamesItsEnclosingDoor(t *testing.T) {
    refusals := 0

    for _, source := range fourthMajorSourceList(t) {
        if true == strings.HasSuffix(source, "_test.go") {
            continue
        }

        fileSet := token.NewFileSet()
        file, parseErr := parser.ParseFile(fileSet, source, nil, 0)
        if nil != parseErr {
            t.Fatalf("parse %s: %v", source, parseErr)
        }

        for _, declaration := range file.Decls {
            function, isFunction := declaration.(*ast.FuncDecl)
            if false == isFunction || nil == function.Body || "RefuseNilRuntime" == function.Name.Name {
                continue
            }

            expected := file.Name.Name + "." + receiverTypeName(function) + function.Name.Name
            handedADoor := functionTakesParameter(function, "door")

            ast.Inspect(function.Body, func(node ast.Node) bool {
                call, isCall := node.(*ast.CallExpr)
                if false == isCall || "RefuseNilRuntime" != calleeName(call.Fun) {
                    return true
                }

                refusals++

                if 2 != len(call.Args) {
                    t.Errorf("%s: RefuseNilRuntime takes the runtime and the door", fileSet.Position(call.Pos()))

                    return true
                }

                switch argument := call.Args[1].(type) {
                case *ast.Ident:
                    if "door" != argument.Name && "lookup" != argument.Name {
                        t.Errorf("%s: the door is a literal or the door the caller hands the helper, got %s", fileSet.Position(call.Pos()), argument.Name)
                    }
                case *ast.BasicLit:
                    if true == handedADoor {
                        t.Errorf("%s: a helper taking its caller's door passes it, got %s", fileSet.Position(call.Pos()), argument.Value)

                        return true
                    }

                    door, unquoteErr := strconv.Unquote(argument.Value)
                    if nil != unquoteErr || expected != door {
                        t.Errorf("%s: expected the door %q, got %s", fileSet.Position(call.Pos()), expected, argument.Value)
                    }
                default:
                    t.Errorf("%s: the door is a literal or the door the caller hands the helper", fileSet.Position(call.Pos()))
                }

                return true
            })
        }
    }

    if 100 > refusals {
        t.Fatalf("expected the fourth major's nil-runtime refusals, found %d", refusals)
    }
}

func receiverTypeName(function *ast.FuncDecl) string {
    if nil == function.Recv || 0 == len(function.Recv.List) {
        return ""
    }

    receiverType := function.Recv.List[0].Type
    if star, isStar := receiverType.(*ast.StarExpr); true == isStar {
        receiverType = star.X
    }

    if indexed, isIndexed := receiverType.(*ast.IndexExpr); true == isIndexed {
        receiverType = indexed.X
    }

    if identifier, isIdentifier := receiverType.(*ast.Ident); true == isIdentifier {
        return identifier.Name + "."
    }

    return ""
}

func functionTakesParameter(function *ast.FuncDecl, name string) bool {
    for _, field := range function.Type.Params.List {
        for _, parameterName := range field.Names {
            if name == parameterName.Name {
                return true
            }
        }
    }

    return false
}
