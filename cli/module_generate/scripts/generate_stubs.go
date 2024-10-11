// Package scripts contains scripts that generate method stubs for modules
package scripts

import (
	"bytes"
	_ "embed"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"net/http"
	"strings"
	"text/template"
	"unicode"

	"github.com/pkg/errors"
	"go.viam.com/utils"

	"go.viam.com/rdk/cli/module_generate/common"
)

//go:embed tmpl-module
var goTmpl string

// getClientCode grabs client.go code of component type.
func getClientCode(module common.ModuleInputs) (string, error) {
	url := fmt.Sprintf("https://raw.githubusercontent.com/viamrobotics/rdk/refs/tags/v%s/%ss/%s/client.go",
		module.SDKVersion, module.ResourceType, module.ResourceSubtype)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", errors.Wrapf(err, "cannot get client code")
	}
	//nolint:bodyclose
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", errors.Wrapf(err, "cannot get client code")
	}
	defer utils.UncheckedErrorFunc(resp.Body.Close)
	if resp.StatusCode != http.StatusOK {
		return "", errors.Errorf("unexpected http GET status: %s getting %s", resp.Status, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return url, errors.Wrapf(err, "error reading response body")
	}
	clientCode := string(body)
	return clientCode, nil
}

// setGoModuleTemplate sets the imports and functions for the go method stubs.
func setGoModuleTemplate(clientCode string, module common.ModuleInputs) (*common.GoModuleTmpl, error) {
	var goTmplInputs common.GoModuleTmpl

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "", clientCode, parser.AllErrors)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse client code")
	}

	var imports []string
	for _, imp := range node.Imports {
		path := imp.Path.Value
		if imp.Name != nil {
			path = fmt.Sprintf("%s %s", imp.Name.Name, path)
		}
		imports = append(imports, path)
	}

	var functions []string
	ast.Inspect(node, func(n ast.Node) bool {
		if funcDecl, ok := n.(*ast.FuncDecl); ok {
			name, args, returns := parseFunctionSignature(module.ResourceSubtype, module.ResourceSubtypePascal, funcDecl)
			if name != "" {
				functions = append(functions, formatEmptyFunction(module.ModuleCamel+module.ModelPascal, name, args, returns))
			}
		}
		return true
	})

	goTmplInputs.Imports = strings.Join(imports, "\n")
	if module.ResourceType == "component" {
		goTmplInputs.ObjName = module.ResourceSubtypePascal
	} else {
		goTmplInputs.ObjName = "Service"
	}
	goTmplInputs.ModelType = module.ModuleCamel + module.ModelPascal
	goTmplInputs.Functions = strings.Join(functions, " ")
	goTmplInputs.Module = module

	return &goTmplInputs, nil
}

// formatType outputs typeExpr as readable string.
func formatType(typeExpr ast.Expr) string {
	var buf bytes.Buffer
	err := printer.Fprint(&buf, token.NewFileSet(), typeExpr)
	if err != nil {
		return fmt.Sprintf("Error formatting type: %v", err)
	}
	return buf.String()
}

// parseFunctionSignature parses function declarations into the function name, the arguments, and the return types.
func parseFunctionSignature(resourceSubtype, resourceSubtypePascal string, funcDecl *ast.FuncDecl) (name, args string, returns []string) {
	if funcDecl == nil {
		return
	}

	// Function name
	funcName := funcDecl.Name.Name
	if !unicode.IsUpper(rune(funcName[0])) {
		return
	}
	if funcName == "Close" || funcName == "Name" || funcName == "Reconfigure" {
		return
	}

	// Parameters
	var params []string
	if funcDecl.Type.Params != nil {
		for _, param := range funcDecl.Type.Params.List {
			paramType := formatType(param.Type)
			for _, name := range param.Names {
				params = append(params, name.Name+" "+paramType)
			}
		}
	}

	// Return types
	if funcDecl.Type.Results != nil {
		for _, result := range funcDecl.Type.Results.List {
			str := formatType(result.Type)
			isPointer := false
			if str[0] == '*' {
				str = str[1:]
				isPointer = true
			}
			// add subtype package name for exported types from that package
			if unicode.IsUpper(rune(str[0])) {
				str = fmt.Sprintf("%s.%s", resourceSubtype, str)
			} else if strings.HasPrefix(str, "[]") && unicode.IsUpper(rune(str[2])) {
				str = fmt.Sprintf("[]%s.%s", resourceSubtype, str[2:])
			} else if str == resourceSubtypePascal {
				str = fmt.Sprintf("%s.%s", resourceSubtype, resourceSubtypePascal)
			}
			if isPointer {
				str = fmt.Sprintf("*%s", str)
			}
			returns = append(returns, str)
		}
	}

	return funcName, strings.Join(params, ", "), returns
}

// formatEmptyFunction outputs the new function that removes the function body, adds the panic unimplemented statement,
// and replaces the receiver with the new model type.
func formatEmptyFunction(receiver, funcName, args string, returns []string) string {
	var returnDef string
	if len(returns) == 0 {
		returnDef = ""
	} else if len(returns) == 1 {
		returnDef = returns[0]
	} else {
		returnDef = fmt.Sprintf("(%s)", strings.Join(returns, ","))
	}
	newFunc := fmt.Sprintf("func (s *%s) %s(%s) %s{\n\tpanic(\"not implemented\")\n}\n\n", receiver, funcName, args, returnDef)
	return newFunc
}

// RenderGoTemplates outputs the method stubs for created module.
func RenderGoTemplates(module common.ModuleInputs) ([]byte, error) {
	clientCode, err := getClientCode(module)
	var empty []byte
	if err != nil {
		return empty, err
	}
	goModule, err := setGoModuleTemplate(clientCode, module)
	if err != nil {
		return empty, err
	}
	var output bytes.Buffer
	tmpl, err := template.New("module").Parse(goTmpl)
	if err != nil {
		return empty, err
	}
	err = tmpl.Execute(&output, goModule)
	if err != nil {
		return empty, err
	}
	return output.Bytes(), nil
}
