package definitions

import (
	"embed"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"sync"

	hatua "hatua.dev/go"
)

// hostManifestFiles holds the Host manifests every Workflow Definition is
// validated against, one Component Manifest file per `component.*.yaml`. They
// ship inside the binary, so every consumer validates against the same
// contract the runner implements.
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
	hostErr       error
)

// HostManifests returns the Component Manifests the Host declares: the
// agentic and script components and the webhook trigger, ordered by use.
func HostManifests() ([]hatua.Manifest, error) {
	hostOnce.Do(loadHost)
	return hostManifests, hostErr
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
		if !strings.HasPrefix(name, "component.") {
			hostErr = fmt.Errorf("host manifest %s: name must start with component.", name)
			return
		}
		ms, err := hatua.LoadManifests(raw)
		if err != nil {
			hostErr = fmt.Errorf("host manifest %s: %w", name, err)
			return
		}
		hostManifests = append(hostManifests, ms...)
	}
	slices.SortFunc(hostManifests, func(a, b hatua.Manifest) int { return strings.Compare(a.Use, b.Use) })
}
