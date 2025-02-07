package template

import (
	"bufio"
	"fmt"
	"html/template"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// ClientInfo consist of group info and path to client.go
type ClientInfo struct {
	Group string // group which client belongs to
	Path  string // path which client.go will be generated
}

// Template consist of message for generate basic http client.
type Template struct {
	Package        string
	Service        string
	PackageImports []template.HTML
}

// FunctionPart consist of essential part to generate http api function.
type FunctionPart struct {
	Action      string
	Request     string
	Response    string
	NewResponse string // struct name without pointer, used to create a new blank return struct.
}

// GetClientInfo return idl generated client.go path, and get which group this client belongs to.
func GetClientInfo(root string) []ClientInfo {
	idlDir := filepath.Join(root, "pkg", "model")
	res := make([]ClientInfo, 0)
	files := make([]string, 0)
	_ = filepath.Walk(idlDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Name() == "client.go" {
			files = append(files, path)
		}
		return nil
	})
	fmt.Println("client.go files", files)
	for _, file := range files {
		base := len(strings.Split(idlDir, "/")) // get base length
		group := strings.Split(file, "/")[base] // base start with '/'
		res = append(res, ClientInfo{
			Group: group,
			Path:  file,
		})
	}
	fmt.Println("clients:", res)
	return res
}

// ProducePath returns the full path of generated file
func ProducePath(root string, client ClientInfo, service string) string {
	return filepath.Join(root, "pkg", "client", client.Group, "generated."+strings.ToLower(service)+".go")
}

// ProduceBaseFile generate client of specific service
// @param     path        		string         "path of generated file"
// @param     pkg         		string         "package name of go file"
// @param     service		    string         "service name"
// @param     pkgImports  		[]string       "imported package list"
func ProduceBaseFile(path, pkg, service string, pkgImports []template.HTML) error {
	baseClient := `// Code generated by vkectl gen_client. DO NOT EDIT.

package {{.Package}}

import (
	"net/url"

	"github.com/volcengine/vkectl/pkg/client"
{{ range .PackageImports }}
	{{.}}
{{- end }}
)

// {{.Service}} is a base client
type {{.Service}} struct {
	Client *client.Client
}

// NewAPIClient returns an api client object
func NewAPIClient(ak, sk, host, service, region string) *{{.Service}} {
	c := client.NewBaseClient()
	c.ServiceInfo = client.NewServiceInfo()
	c.ServiceInfo.Host = host
	c.ServiceInfo.Credentials.AccessKeyID = ak
	c.ServiceInfo.Credentials.SecretAccessKey = sk
	c.ServiceInfo.Credentials.Service = service
	c.ServiceInfo.Credentials.Region = region
	c.SdkVersion = client.DefaultSdkVersion

	return &{{.Service}}{Client: c}
}
`
	temp, err := template.New("base").Parse(baseClient)
	if err != nil {
		return err
	}

	value := Template{
		Package:        pkg,
		Service:        service,
		PackageImports: pkgImports,
	}
	dir := filepath.Dir(path)
	err = os.MkdirAll(dir, 0755)
	if err != nil && os.IsExist(err) {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	return temp.Execute(file, value)
}

// FunctionJointForOpenAPI joint function part to a complete function special for api test.
func FunctionJointForOpenAPI(service string, functionPart []FunctionPart) string {
	baseFunc := `
func (p *%s) %s(body string, query url.Values) (%s, statusCode int, err error) {
	action := "%s"
	r = %s
	statusCode, err = p.Client.CommonHandler(action, query, body, r)
	return r, statusCode, err
}
`
	res := ""
	for _, part := range functionPart {
		action := part.Action
		resp := part.Response
		newResp := part.NewResponse
		finalFunc := fmt.Sprintf(baseFunc, service, action, resp, action, newResp)
		res += finalFunc
	}

	return res
}

// GetFuncPart get the service, Action, Request, Response and Response struct from idl generated function.
func GetFuncPart(filename string) (string, []FunctionPart, []template.HTML, error) {
	file, _ := os.Open(filename)
	defer func() {
		_ = file.Close() // we do not write to this file, so don't care about error.
	}()

	content, err := ioutil.ReadFile(filename)
	if err != nil {
		fmt.Printf("read file %v faild, err is %v\n", filename, err)
		return "", nil, nil, err
	}

	svc := regexp.MustCompile(`package (.*?)\n`)
	service := strings.Title(svc.FindStringSubmatch(string(content))[1])
	fmt.Printf("service = *%v\n", service)

	imps := regexp.MustCompile(`"(.*/kitex_gen/.*)"`)
	imports := imps.FindAllString(string(content), -1)

	action, err := regexp.Compile(`(?s)type Client interface {\n(.*?)\n}`)
	if err != nil {
		fmt.Printf("compile reg failed, %v\n", err)
		return "", nil, nil, err
	}

	actionstring := action.FindStringSubmatch(string(content))
	funcstring := actionstring[1]
	funcs := strings.Split(funcstring, "\n")
	funcpart := make([]FunctionPart, 0)
	for _, function := range funcs {
		part := FunctionPart{}
		part.Action = strings.TrimSpace(strings.Split(function, "(")[0])
		part.Request = strings.Split(function, ", ")[1]
		part.Response = strings.Split(strings.Split(function, "(")[2], ",")[0]
		// req *package.Response, => &package.Response{}
		part.NewResponse = "&" + strings.Split(strings.Split(part.Response, "*")[1], ",")[0] + "{}"
		funcpart = append(funcpart, part)
	}
	return service, funcpart, func(src []string) []template.HTML {
		res := make([]template.HTML, 0, len(src))
		for _, s := range src {
			res = append(res, template.HTML(s))
		}
		return res
	}(imports), nil
}

// GetImportPkgName get the idl generated client package which will import to http client file.
func GetImportPkgName(root, module, path string) string {
	fmt.Println("root=", root)
	relative := strings.Split(path, root)[1]
	fmt.Println("relative = ", relative)
	pathList := strings.Split(relative, string(os.PathSeparator))
	fmt.Println("pathlist=", pathList)
	return filepath.Join(module, filepath.Join(pathList[:len(pathList)-2]...))
}

// WriteFuncs write generated http function to give file.
func WriteFuncs(path, funcs string) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_RDWR, 0666)
	if err != nil {
		return err
	}
	writer := bufio.NewWriter(file)
	_, err = writer.WriteString(funcs)
	if err != nil {
		return err
	}
	return writer.Flush()
}

// GetModule return module from go.mod in repo
func GetModule(root string) (string, error) {
	modDir := path.Join(root, "go.mod")
	file, err := os.Open(modDir)
	if err != nil {
		return "", fmt.Errorf("[ERROR] %v", err)
	}
	defer func() { _ = file.Close() }()

	fileCanner := bufio.NewScanner(file)
	r, _ := regexp.Compile(`module\s+(\S*)`)
	for fileCanner.Scan() {
		if r.MatchString(fileCanner.Text()) {
			module := r.FindStringSubmatch(fileCanner.Text())[1]
			return strings.TrimSpace(module), nil
		}
	}
	return "", fmt.Errorf("[ERROR] find no module in file %s, please check", modDir)
}
