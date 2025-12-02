package localstate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/docker/buildx/util/confutil"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

const (
	version  = 2
	refsDir  = "refs"
	groupDir = "__group__"
)

type State struct {
	// Target is the name of the invoked target (default if empty)
	Target string
	// LocalPath is the absolute path to the context or remote context
	LocalPath string
	// DockerfilePath is the absolute path to the Dockerfile or relative if
	// context is remote
	DockerfilePath string
	// GroupRef is the ref of the state group that this ref belongs to
	GroupRef string `json:",omitempty"`
}

type StateGroup struct {
	// Targets are the targets invoked
	Targets []string `json:",omitempty"`
	// Refs are used to track all the refs that belong to the same group
	Refs []string
}

type LocalState struct {
	cfg *confutil.Config
}

func New(cfg *confutil.Config) (*LocalState, error) {
	if cfg.Dir() == "" {
		return nil, errors.Errorf("config dir empty")
	}
	if err := cfg.MkdirAll(refsDir, 0700); err != nil {
		return nil, err
	}
	return &LocalState{cfg: cfg}, nil
}

func (ls *LocalState) ReadRef(builderName, nodeName, id string) (*State, error) {
	if err := ls.validate(builderName, nodeName, id); err != nil {
		return nil, err
	}
	dt, err := os.ReadFile(filepath.Join(ls.cfg.Dir(), refsDir, builderName, nodeName, id))
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(dt, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (ls *LocalState) SaveRef(builderName, nodeName, id string, st State) error {
	if err := ls.validate(builderName, nodeName, id); err != nil {
		return err
	}
	refDir := filepath.Join(refsDir, builderName, nodeName)
	if err := ls.cfg.MkdirAll(refDir, 0700); err != nil {
		return err
	}
	dt, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return ls.cfg.AtomicWriteFile(filepath.Join(refDir, id), dt, 0644)
}

func (ls *LocalState) GroupDir() string {
	return filepath.Join(ls.cfg.Dir(), refsDir, groupDir)
}

func (ls *LocalState) ReadGroup(id string) (*StateGroup, error) {
	dt, err := os.ReadFile(filepath.Join(ls.GroupDir(), id))
	if err != nil {
		return nil, err
	}
	var stg StateGroup
	if err := json.Unmarshal(dt, &stg); err != nil {
		return nil, err
	}
	return &stg, nil
}

func (ls *LocalState) SaveGroup(id string, stg StateGroup) error {
	refDir := filepath.Join(refsDir, groupDir)
	if err := ls.cfg.MkdirAll(refDir, 0700); err != nil {
		return err
	}
	dt, err := json.Marshal(stg)
	if err != nil {
		return err
	}
	return ls.cfg.AtomicWriteFile(filepath.Join(refDir, id), dt, 0600)
}

func (ls *LocalState) RemoveBuilder(builderName string) error {
	if builderName == "" {
		return errors.Errorf("builder name empty")
	}

	dir := filepath.Join(ls.cfg.Dir(), refsDir, builderName)
	if _, err := os.Lstat(dir); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	fis, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, fi := range fis {
		if err := ls.RemoveBuilderNode(builderName, fi.Name()); err != nil {
			return err
		}
	}

	return os.RemoveAll(dir)
}

// RemoveBuilderNode removes all refs for a builder node.
// This func is not safe for concurrent use from multiple goroutines.
func (ls *LocalState) RemoveBuilderNode(builderName string, nodeName string) error {
	if builderName == "" {
		return errors.Errorf("builder name empty")
	}
	if nodeName == "" {
		return errors.Errorf("node name empty")
	}

	dir := filepath.Join(ls.cfg.Dir(), refsDir, builderName, nodeName)
	if _, err := os.Lstat(dir); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	fis, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	var murefs sync.Mutex
	grefs := make(map[string][]string)
	srefs := make(map[string][]string)
	eg, _ := errgroup.WithContext(context.TODO())
	for _, fi := range fis {
		func(fi os.DirEntry) {
			eg.Go(func() error {
				st, err := ls.ReadRef(builderName, nodeName, fi.Name())
				if err != nil {
					return err
				}
				if st.GroupRef == "" {
					return nil
				}
				murefs.Lock()
				defer murefs.Unlock()
				if _, ok := grefs[st.GroupRef]; !ok {
					if grp, err := ls.ReadGroup(st.GroupRef); err == nil {
						grefs[st.GroupRef] = grp.Refs
					}
				}
				srefs[st.GroupRef] = append(srefs[st.GroupRef], fmt.Sprintf("%s/%s/%s", builderName, nodeName, fi.Name()))
				return nil
			})
		}(fi)
	}
	if err := eg.Wait(); err != nil {
		return err
	}

	for gid, refs := range grefs {
		if s, ok := srefs[gid]; ok {
			if len(s) != len(refs) {
				continue
			}
			if err := ls.removeGroup(gid); err != nil {
				return err
			}
		}
	}

	return os.RemoveAll(dir)
}

func (ls *LocalState) removeGroup(id string) error {
	if id == "" {
		return errors.Errorf("group ref empty")
	}
	f := filepath.Join(ls.GroupDir(), id)
	if _, err := os.Lstat(f); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return os.Remove(f)
}

func (ls *LocalState) validate(builderName, nodeName, id string) error {
	if builderName == "" {
		return errors.Errorf("builder name empty")
	}
	if nodeName == "" {
		return errors.Errorf("node name empty")
	}
	if id == "" {
		return errors.Errorf("ref ID empty")
	}
	return nil
}

func (ls *LocalState) readVersion() int {
	if vdt, err := os.ReadFile(filepath.Join(ls.cfg.Dir(), refsDir, "version")); err == nil {
		if v, err := strconv.Atoi(string(vdt)); err == nil {
			return v
		}
	}
	return 1
}

func (ls *LocalState) writeVersion(version int) error {
	return ls.cfg.AtomicWriteFile(filepath.Join(refsDir, "version"), []byte(strconv.Itoa(version)), 0600)
}
