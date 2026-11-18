package definitions

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/goccy/go-yaml"
	hatua "hatua.dev/go"
	"hatua.dev/go/expressions"
)

// hostManifestFiles holds the Host manifests every Workflow Definition is
// validated against: one Component Manifest per `component.*.yaml` and one
// Function Manifest per `function.*.yaml`. They ship inside the binary, so
// every consumer validates against the same contract the runner implements.
//
//go:embed hostmanifests/*.yaml
var hostManifestFiles embed.FS

// HostManifestFS is the embedded Host manifests as files, each named
// hostmanifests/<file>.yaml and holding the YAML exactly as shipped.
func HostManifestFS() fs.FS {
	sub, err := fs.Sub(hostManifestFiles, "hostmanifests")
	if err != nil {
		panic(err) // the directory is embedded; Sub cannot fail on it
	}
	return sub
}

var (
	hostOnce      sync.Once
	hostManifests []hatua.Manifest
	hostFunctions []expressions.FunctionSpec
	hostErr       error
)

// HostManifests returns the Component Manifests the Host declares: the
// agentic and script components and the webhook trigger, ordered by use.
func HostManifests() ([]hatua.Manifest, error) {
	hostOnce.Do(loadHost)
	return hostManifests, hostErr
}

// HostFunctions returns the function signatures the Host declares, which an
// Expression may call alongside hatua's own.
func HostFunctions() ([]expressions.FunctionSpec, error) {
	hostOnce.Do(loadHost)
	return hostFunctions, hostErr
}

func loadHost() {
	entries, err := fs.ReadDir(hostManifestFiles, "hostmanifests")
	if err != nil {
		hostErr = err
		return
	}
	for _, e := range entries {
		name := e.Name()
		raw, err := hostManifestFiles.ReadFile("hostmanifests/" + name)
		if err != nil {
			hostErr = err
			return
		}
		switch {
		case strings.HasPrefix(name, "component."):
			ms, err := hatua.LoadManifests(raw)
			if err != nil {
				hostErr = fmt.Errorf("host manifest %s: %w", name, err)
				return
			}
			hostManifests = append(hostManifests, ms...)
		case strings.HasPrefix(name, "function."):
			specs, err := loadFunctionManifest(raw)
			if err != nil {
				hostErr = fmt.Errorf("host manifest %s: %w", name, err)
				return
			}
			hostFunctions = append(hostFunctions, specs...)
		default:
			hostErr = fmt.Errorf("host manifest %s: name must start with component. or function.", name)
			return
		}
	}
	sort.Slice(hostManifests, func(i, j int) bool { return hostManifests[i].Use < hostManifests[j].Use })
}

// functionManifest is one namespace of hatua's Function Manifest format.
// hatua.dev/go reads Component Manifests and has no reader for this one, so
// its shape is declared here and decoded strictly: a key the format does not
// define is an error, as hatua's own readers make it.
type functionManifest struct {
	Kind      string         `yaml:"kind"`
	Namespace string         `yaml:"namespace"`
	Summary   string         `yaml:"summary"`
	Functions []functionSpec `yaml:"functions"`
}

type functionSpec struct {
	Name    string          `yaml:"name"`
	Summary string          `yaml:"summary"`
	Params  []functionParam `yaml:"params"`
	Returns string          `yaml:"returns"`
}

type functionParam struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Description string `yaml:"description"`
	Optional    bool   `yaml:"optional"`
	Variadic    bool   `yaml:"variadic"`
}

// functionNamePattern is the Function Manifest's pattern for a namespace, a
// function name and a parameter name.
var functionNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// functionValueTypes is the Function Manifest's value types.
var functionValueTypes = map[string]bool{
	"text": true, "number": true, "boolean": true, "datetime": true, "list": true,
	"object": true, "item": true, "unknown": true, "null": true,
}

func loadFunctionManifest(raw []byte) ([]expressions.FunctionSpec, error) {
	var m functionManifest
	dec := yaml.NewDecoder(bytes.NewReader(raw), yaml.Strict())
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("not a valid Function Manifest: %w", err)
	}
	if m.Kind != "function" {
		return nil, fmt.Errorf("not a valid Function Manifest: kind must be function, got %q", m.Kind)
	}
	if !functionNamePattern.MatchString(m.Namespace) {
		return nil, fmt.Errorf("not a valid Function Manifest: namespace %q is not a name", m.Namespace)
	}
	if len(m.Functions) == 0 {
		return nil, fmt.Errorf("not a valid Function Manifest: namespace %q declares no functions", m.Namespace)
	}
	specs := make([]expressions.FunctionSpec, 0, len(m.Functions))
	for _, f := range m.Functions {
		qualified := m.Namespace + "." + f.Name
		if !functionNamePattern.MatchString(f.Name) {
			return nil, fmt.Errorf("not a valid Function Manifest: function %q is not a name", qualified)
		}
		if !functionValueTypes[f.Returns] {
			return nil, fmt.Errorf("not a valid Function Manifest: %s returns unknown type %q", qualified, f.Returns)
		}
		params := make([]expressions.ParamSpec, 0, len(f.Params))
		optional := false
		for i, p := range f.Params {
			if optional && !p.Optional {
				return nil, fmt.Errorf("not a valid Function Manifest: %s parameter %s follows an optional one", qualified, p.Name)
			}
			optional = optional || p.Optional
			if !functionNamePattern.MatchString(p.Name) {
				return nil, fmt.Errorf("not a valid Function Manifest: %s parameter %q is not a name", qualified, p.Name)
			}
			if !functionValueTypes[p.Type] {
				return nil, fmt.Errorf("not a valid Function Manifest: %s parameter %s has unknown type %q", qualified, p.Name, p.Type)
			}
			if p.Variadic && i != len(f.Params)-1 {
				return nil, fmt.Errorf("not a valid Function Manifest: %s parameter %s is variadic and not last", qualified, p.Name)
			}
			params = append(params, expressions.ParamSpec{
				Name:        p.Name,
				Type:        expressions.ValueType(p.Type),
				Description: p.Description,
				Optional:    p.Optional,
				Variadic:    p.Variadic,
			})
		}
		specs = append(specs, expressions.FunctionSpec{
			Namespace: m.Namespace,
			Name:      f.Name,
			Qualified: qualified,
			Summary:   f.Summary,
			Params:    params,
			Returns:   expressions.ValueType(f.Returns),
		})
	}
	return specs, nil
}
