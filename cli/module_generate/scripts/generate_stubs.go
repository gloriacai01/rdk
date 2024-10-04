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

	"go.viam.com/rdk/cli/module_generate/common"

	"github.com/pkg/errors"
)

//go:embed tmpl-module
var goTmpl string

// getClientCode grabs client.go code of component type
func getClientCode(module common.ModuleInputs) (string, error) {
	url := fmt.Sprintf("https://raw.githubusercontent.com/viamrobotics/rdk/refs/tags/v%s/%ss/%s/client.go", module.SDKVersion, module.ResourceType, module.ResourceSubtype)
	resp, err := http.Get(url)
	if err != nil {
		return "", errors.Wrapf(err, "cannot get latest release")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", errors.Errorf("unexpected http GET status: %s getting %s", resp.Status, url)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return url, errors.Errorf("Error reading response body:", err)
	}
	clientCode := string(body)

	return clientCode, nil
}

// setGoModuleTemplate sets the imports and functions for the go method stubs
func setGoModuleTemplate(clientCode string, module common.ModuleInputs) common.GoModuleTmpl {
	var goTmplInputs common.GoModuleTmpl
	start := strings.Index(clientCode, "(\n")

	end := strings.Index(clientCode, ")")
	// if start == -1 || end == -1 || start >= end {
	// 	fmt.Println("No imports found.")
	// 	return
	// }

	imports := clientCode[start+1 : end]
	replacements := []string{
		"rprotoutils \"go.viam.com/rdk/protoutils\"\n",
		"\"go.viam.com/rdk/protoutils\"",
		"commonpb \"go.viam.com/api/common/v1\"\n",
		"\"go.viam.com/utils/protoutils\"\n",
		"\"google.golang.org/protobuf/types/known/structpb\"\n",
		"\"fmt\"",
		"\"go.viam.com/utils/rpc\"\n",
		// fmt.Sprintf("pb \"go.viam.com/api/component/%s/v1\"\n", module.ResourceSubtype),
		"\"errors\"",
	}

	for _, replacement := range replacements {
		imports = strings.ReplaceAll(imports, replacement, "")
	}

	goTmplInputs.Imports = imports

	if module.ResourceType == "component" {
		goTmplInputs.ObjName = module.ResourceSubtypePascal
	} else {
		goTmplInputs.ObjName = "Service"
	}
	goTmplInputs.ModelType = module.ModuleCamel + module.ModelPascal
	goTmplInputs.Functions = parseFuncs(module.ResourceSubtype, module.ResourceSubtypePascal, goTmplInputs.ModelType, clientCode)
	goTmplInputs.Module = module

	return goTmplInputs
}

// formatType outputs typeExpr as string
func formatType(typeExpr ast.Expr) string {
	var buf bytes.Buffer
	err := printer.Fprint(&buf, token.NewFileSet(), typeExpr)
	if err != nil {
		return fmt.Sprintf("Error formatting type: %v", err)
	}
	return buf.String()
}

// // newReturnStatement returns the appropiate return statement with nils, errUnimplemented, empty structs
// func newReturnStatement(resourceSubtype string, returns []string) string {
// 	for i, r := range returns {
// 		if r == "bool" {
// 			returns[i] = "false"
// 		} else if r[0] == '*' {
// 			returns[i] = "nil"
// 		} else if strings.Contains(r, "float") {
// 			returns[i] = "-1"
// 		} else if r == "string" {
// 			returns[i] = "\"\""
// 		} else if strings.Contains(r, "error") {
// 			returns[i] = "errUnimplemented"
// 		} else if strings.Contains(r, "Properties") {
// 			returns[i] = resourceSubtype + ".Properties{}"
// 		} else if strings.Contains(r, "Accuracy") {
// 			returns[i] = resourceSubtype + ".Accuracy{}"
// 		} else if strings.Contains(r, "r3.Vector") {
// 			returns[i] = "r3.Vector{}"
// 		} else if strings.Contains(r, "spatialmath.AngularVelocity") {
// 			returns[i] = "spatialmath.AngularVelocity{}"
// 		} else if strings.Contains(r, "func") {
// 			returns[i] = "nil"
// 		} else {
// 			returns[i] = "nil"
// 		}
// 	}
// 	return fmt.Sprintf("return %s", strings.Join(returns, ", "))
// }

// parseFunctionSignature parses function declarations into the function name, the arguments, and the return types
func parseFunctionSignature(resourceSubtype string, resourceSubtypePascal string, funcDecl *ast.FuncDecl) (name string, args string, returns []string) {
	if funcDecl == nil {
		return
	}

	// Function name
	funcName := funcDecl.Name.Name
	if !unicode.IsUpper(rune(funcName[0])) {
		return
	}
	if funcName == "Close" {
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

// formatEmptyFunction outputs the new function that removes the function body, adds the new return statement with errUnimplemented, and replaces the receiver with the new model type
func formatEmptyFunction(receiver string, resourceSubtype string, funcName string, args string, returns []string) string {
	var returnDef string
	if len(returns) == 0 {
		returnDef = ""
	} else if len(returns) == 1 {
		returnDef = returns[0]
	} else {
		returnDef = fmt.Sprintf("(%s)", strings.Join(returns, ","))
	}
	// newReturn := newReturnStatement(resourceSubtype, returns)
	newFunc := fmt.Sprintf("func (s *%s) %s(%s) %s{\n\tpanic(\"not implemented\")\n}\n\n", receiver, funcName, args, returnDef)
	return newFunc

}

// parseFuncs inspects the client code as an AST, parses the functions, and outputs the method stubs
func parseFuncs(resourceSubtype string, resourceSubtypePascal string, modelType string, code string) string {
	var functions []string
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "", code, parser.AllErrors)
	if err != nil {
		fmt.Println("Error parsing source:", err)
		return ""
	}

	ast.Inspect(node, func(n ast.Node) bool {
		if funcDecl, ok := n.(*ast.FuncDecl); ok {
			name, args, returns := parseFunctionSignature(resourceSubtype, resourceSubtypePascal, funcDecl)
			if name != "" {
				functions = append(functions, formatEmptyFunction(modelType, resourceSubtype, name, args, returns))
			}
		}
		return true
	})
	return strings.Join(functions, " ")
}

// RenderGoTemplates outputs the method stubs for module
func RenderGoTemplates(module common.ModuleInputs) ([]byte, error) {
	clientCode, err := getClientCode(module)
	var empty []byte
	if err != nil {
		fmt.Print(err)
		return empty, err
	}
	goModule := setGoModuleTemplate(clientCode, module)
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
