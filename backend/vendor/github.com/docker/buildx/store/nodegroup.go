package store

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/containerd/platforms"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/platformutil"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type NodeGroup struct {
	Name    string
	Driver  string
	Nodes   []Node
	Dynamic bool

	// skip the following fields from being saved in the store
	DockerContext bool      `json:"-"`
	LastActivity  time.Time `json:"-"`
}

type Node struct {
	Name           string
	Endpoint       string
	Platforms      []ocispecs.Platform
	DriverOpts     map[string]string
	BuildkitdFlags []string `json:"Flags"` // keep the field name for backward compatibility

	Files map[string][]byte
}

func (ng *NodeGroup) Leave(name string) error {
	if ng.Dynamic {
		return errors.New("dynamic node group does not support Leave")
	}
	i := ng.findNode(name)
	if i == -1 {
		return errors.Errorf("node %q not found for %s", name, ng.Name)
	}
	if len(ng.Nodes) == 1 {
		return errors.Errorf("can not leave last node, do you want to rm instance instead?")
	}
	ng.Nodes = slices.Delete(ng.Nodes, i, i+1)
	return nil
}

func (ng *NodeGroup) Update(name, endpoint string, platforms []string, endpointsSet bool, actionAppend bool, buildkitdFlags []string, buildkitdConfigFile string, do map[string]string) error {
	if ng.Dynamic {
		return errors.New("dynamic node group does not support Update")
	}
	i := ng.findNode(name)
	if i == -1 && !actionAppend {
		if len(ng.Nodes) > 0 {
			return errors.Errorf("node %s not found, did you mean to append?", name)
		}
		ng.Nodes = nil
	}

	pp, err := platformutil.Parse(platforms)
	if err != nil {
		return err
	}

	var files map[string][]byte
	if buildkitdConfigFile != "" {
		files, err = confutil.LoadConfigFiles(buildkitdConfigFile)
		if err != nil {
			return err
		}
	}

	if i != -1 {
		n := ng.Nodes[i]
		needsRestart := false
		if endpointsSet {
			n.Endpoint = endpoint
			needsRestart = true
		}
		if len(platforms) > 0 {
			n.Platforms = pp
		}
		if buildkitdFlags != nil {
			n.BuildkitdFlags = buildkitdFlags
			needsRestart = true
		}
		if do != nil {
			n.DriverOpts = do
			needsRestart = true
		}
		if buildkitdConfigFile != "" {
			maps.Copy(n.Files, files)
			needsRestart = true
		}
		if needsRestart {
			logrus.Warn("new settings may not be used until builder is restarted")
		}

		ng.Nodes[i] = n
		return ng.validateDuplicates(endpoint, i)
	}

	if name == "" {
		name = ng.nextNodeName()
	}

	name, err = ValidateName(name)
	if err != nil {
		return err
	}

	n := Node{
		Name:           name,
		Endpoint:       endpoint,
		Platforms:      pp,
		DriverOpts:     do,
		BuildkitdFlags: buildkitdFlags,
		Files:          files,
	}

	ng.Nodes = append(ng.Nodes, n)
	return ng.validateDuplicates(endpoint, len(ng.Nodes)-1)
}

func (ng *NodeGroup) Copy() *NodeGroup {
	nodes := make([]Node, len(ng.Nodes))
	for i, node := range ng.Nodes {
		nodes[i] = *node.Copy()
	}
	return &NodeGroup{
		Name:    ng.Name,
		Driver:  ng.Driver,
		Nodes:   nodes,
		Dynamic: ng.Dynamic,
	}
}

func (n *Node) Copy() *Node {
	platforms := []ocispecs.Platform{}
	copy(platforms, n.Platforms)
	buildkitdFlags := []string{}
	copy(buildkitdFlags, n.BuildkitdFlags)
	driverOpts := map[string]string{}
	maps.Copy(driverOpts, n.DriverOpts)
	files := map[string][]byte{}
	for k, v := range n.Files {
		vv := []byte{}
		copy(vv, v)
		files[k] = vv
	}
	return &Node{
		Name:           n.Name,
		Endpoint:       n.Endpoint,
		Platforms:      platforms,
		BuildkitdFlags: buildkitdFlags,
		DriverOpts:     driverOpts,
		Files:          files,
	}
}

func (ng *NodeGroup) validateDuplicates(ep string, idx int) error {
	i := 0
	for _, n := range ng.Nodes {
		if n.Endpoint == ep {
			i++
		}
	}
	if i > 1 {
		return errors.Errorf("invalid duplicate endpoint %s", ep)
	}

	m := map[string]struct{}{}
	for _, p := range ng.Nodes[idx].Platforms {
		m[platforms.Format(p)] = struct{}{}
	}

	for i := range ng.Nodes {
		if i == idx {
			continue
		}
		ng.Nodes[i].Platforms = filterPlatforms(ng.Nodes[i].Platforms, m)
	}

	return nil
}

func (ng *NodeGroup) findNode(name string) int {
	for i, n := range ng.Nodes {
		if n.Name == name {
			return i
		}
	}
	return -1
}

func (ng *NodeGroup) nextNodeName() string {
	i := 0
	for {
		name := fmt.Sprintf("%s%d", ng.Name, i)
		if ii := ng.findNode(name); ii != -1 {
			i++
			continue
		}
		return name
	}
}

func filterPlatforms(in []ocispecs.Platform, m map[string]struct{}) []ocispecs.Platform {
	out := make([]ocispecs.Platform, 0, len(in))
	for _, p := range in {
		if _, ok := m[platforms.Format(p)]; !ok {
			out = append(out, p)
		}
	}
	return out
}
