package scripts

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/template"
	"time"

	"github.com/pkg/errors"
)

//go:embed tmpl-module
var goTmpl string

// ModuleInputs contains the necessary information to fill out template files.
type ModuleInputs struct {
	ModuleName       string    `json:"module_name"`
	IsPublic         bool      `json:"-"`
	Namespace        string    `json:"namespace"`
	Language         string    `json:"language"`
	Resource         string    `json:"-"`
	ResourceType     string    `json:"resource_type"`
	ResourceSubtype  string    `json:"resource_subtype"`
	ModelName        string    `json:"model_name"`
	EnableCloudBuild bool      `json:"enable_cloud_build"`
	InitializeGit    bool      `json:"initialize_git"`
	RegisterOnApp    bool      `json:"-"`
	GeneratorVersion string    `json:"generator_version"`
	GeneratedOn      time.Time `json:"generated_on"`

	ModulePascal          string `json:"-"`
	ModuleCamel           string `json:"-"`
	ModuleLowercase       string `json:"-"`
	API                   string `json:"-"`
	ResourceSubtypePascal string `json:"-"`
	ResourceTypePascal    string `json:"r-"`
	ModelPascal           string `json:"-"`
	ModelCamel            string `json:"-"`
	ModelTriple           string `json:"-"`
	ModelLowercase        string `json:"-"`

	SDKVersion string `json:"-"`
}

type GoModuleTmpl struct {
	Module    ModuleInputs
	ModelType string
	ObjName   string
	Imports   string
	Functions string
}

func getClientCode(module ModuleInputs) (string, error) {
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

func setGoModuleTemplate(clientCode string, module ModuleInputs) GoModuleTmpl {
	var goTmplInputs GoModuleTmpl
	start := strings.Index(clientCode, "(\n")

	end := strings.Index(clientCode, ")")
	// if start == -1 || end == -1 || start >= end {
	// 	fmt.Println("No imports found.")
	// 	return
	// }

	imports := clientCode[start+1 : end] // +2 to skip '(\n'
	replacements := []string{
		"rprotoutils \"go.viam.com/rdk/protoutils\"\n",
		// `rprotoutils "go.viam.com/rdk/protoutils"\n`,
		// `rprotoutils`,
		"\"go.viam.com/utils/protoutils\"\n",
		"\"google.golang.org/protobuf/types/known/structpb\"\n",
		"\"go.viam.com/utils/rpc\"\n",
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

	functions := strings.Split(clientCode, "func (c *client)")[1:]

	for i, function := range functions {
		functions[i] = fmt.Sprintf("func (s *%s)", goTmplInputs.ModelType) + function
		start := strings.Index(functions[i], "{\n")
		funcDef := functions[i][:start+1]
		numParen := strings.Count(funcDef, ")")
		returnString := ""
		if numParen >= 3 {
			lastOpenParen := strings.LastIndex(funcDef, "(")
			returns := strings.TrimSpace(funcDef[lastOpenParen+1 : start-2])
			slice := strings.Split(returns, ",")

			for i, v := range slice {
				if v == "bool" {
					slice[i] = "false"
				} else if strings.Contains(slice[i], "error") {
					slice[i] = " errUnimplemented"
				} else {
					slice[i] = "nil"
				}
			}
			returnString = strings.Join(slice, ",")

		} else if strings.Contains(funcDef, "error") {
			returnString = " errUnimplemented"
		} else {
			returnString = " nil"
		}
		end := strings.LastIndex(functions[i], "}")
		if start != -1 && end != -1 {
			inside := functions[i][start+1 : end]
			returnStatement := fmt.Sprintf("\n\treturn %s\n", returnString)
			functions[i] = strings.Replace(functions[i], inside, returnStatement, 1)
		}

	}
	goTmplInputs.Functions = strings.Join(functions, "")
	goTmplInputs.Module = module

	return goTmplInputs
}

func RenderGoTemplates(module ModuleInputs) ([]byte, error) {
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
