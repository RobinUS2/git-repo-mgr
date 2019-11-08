package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"
	"time"
)

// rm ../.*.json

// compress
// remove & clone
// metadata file
// conf for exclusions / whitelist
// exclude ourselves
const GitBinaryPath = "/usr/bin/git"

type Instance struct {
	conf *Conf
	Cwd string // current working directory path

	gitCmdConcurrencyThrottle chan bool
}

type Conf struct {
	Path string
}

func (conf *Conf) validate() error {
	if conf.Path == "" {
		conf.Path = "./"
	}
	return nil
}

func (i *Instance) Run() error {
	if err := i.init(); err != nil {
		return err
	}

	files, err := ioutil.ReadDir(i.conf.Path)
	if err != nil {
		log.Fatal(err)
	}

	var errList = make([]error, 0)
	var errorsMux sync.RWMutex
	wg := sync.WaitGroup{}
	for _, f := range files {
		wg.Add(1)
		go func(f os.FileInfo) {
			defer wg.Done()
			err := i.handleFile(f)
			if err != nil {
				err = fmt.Errorf("%s error: %s", f.Name(), err)
				errorsMux.Lock()
				errList = append(errList, err)
				errorsMux.Unlock()
			}
		}(f)
	}
	wg.Wait()

	if len(errList) > 0 {
		var errStr bytes.Buffer
		for _, err := range errList {
			errStr.WriteString(err.Error())
		}
		return errors.New(errStr.String())
	}

	return nil
}

func (i *Instance) init() error {
	if err := i.loadConf(); err != nil {
		return err
	}
	i.Cwd, _ = os.Getwd()
	return nil
}

func (i *Instance) loadConf() error {
	var conf Conf
	b, _ := ioutil.ReadFile(".git-repo-mgr")
	if len(b) > 0 {
		// config is optional, but if we have one it should be valid
		if err := json.Unmarshal(b, &conf); err != nil {
			return err
		}
		i.conf = &conf
	}
	if err := i.conf.validate(); err != nil {
		return err
	}
	return nil
}

func (i *Instance) handleFile(fileInfo os.FileInfo) error {
	if !fileInfo.IsDir() {
		// ignore
		return nil
	}
	path := i.conf.Path + fileInfo.Name()
	cwd := Cwd(path)

	if strings.Contains(path, "git-repo-mgr") {
		// skip ourselves
		return nil
	}

	// test git path
	gitPath := path + "/.git"
	gitPathF, gitErr := os.Open(gitPath)
	if os.IsNotExist(gitErr) {
		// not git
		// @todo check compressed
		return nil
	}
	log.Println(cwd.StatePath())

	// clean?
	if clean, err := i.GitIsClean(cwd); err != nil || !clean {
		// no need to pass error, but not clean is not good
		return nil
	}

	// state
	state, err := i.GetOrCreateState(cwd)
	if err != nil {
		if err.Error() == ErrNoOrigin {
			// just skip
			return nil
		}
		return err
	}
	log.Printf("%+v %s state", state, err)

	// update once a day
	if time.Since(state.Updated) > 24 * time.Hour {
		// update
		if err := i.UpdateStateFromGit(state); err != nil {
			return err
		}
		// persist
		if err := i.PutState(state); err != nil {
			return err
		}
	}

	// @todo check last used

	log.Printf("%s state %v %v", path, gitPathF, gitErr)

	return nil
}

func (i *Instance) GetOrCreateState(cwd Cwd) (state *StateDetails, err error) {
	s, err := i.GetState(cwd)
	if s != nil && err == nil {
		return s, nil
	}

	state = &StateDetails{}
	state.ManagerPath = i.Cwd
	state.Cwd = cwd.String()
	state.Created = time.Now()
	if err := i.UpdateStateFromGit(state); err != nil {
		return nil, err
	}

	return state, i.PutState(state)
}

const ErrNoOrigin = "no origin"

func (i *Instance) UpdateStateFromGit(state *StateDetails) (err error) {
	if state == nil {
		panic("missing state")
	}

	state.GitBranch, err = i.GitBranch(state.GetCwd())
	if err != nil {
		return  err
	}

	state.GitOrigin, err = i.GitOrigin(state.GetCwd())
	if err != nil {
		return errors.New(ErrNoOrigin)
	}

	state.GitLastUpdate, err = i.GitLastCommitTime(state.GetCwd())
	if err != nil {
		return  err
	}

	return nil
}

func (i *Instance) GetState(cwd Cwd) (state *StateDetails, err error) {
	b, err := ioutil.ReadFile(cwd.StatePath())
	if err != nil {
		return nil, err
	}
	state = &StateDetails{}
	if err := state.FromBytes(b); err != nil {
		return nil, err
	}
	return state, nil
}

func (i *Instance) PutState(updated *StateDetails) error {
	updated.Updated = time.Now()
	return ioutil.WriteFile(updated.GetCwd().StatePath(), updated.ToBytes(), 0644)
}

func (s *StateDetails) GetCwd() Cwd {
	return Cwd(s.Cwd)
}

func (s *StateDetails) FromBytes(b []byte) error {
	return json.Unmarshal(b, &s)
}

func (s StateDetails) ToBytes() []byte {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return b
}

type StateDetails struct {
	ManagerPath string // path of where git-repo-mgr was called
	Cwd          string    // path relative to the git-repo-mgr
	GitOrigin    string    // link to git origin
	GitBranch    string    // branch we were on
	GitLastUpdate time.Time // last git commit time
	Created      time.Time // updated on first creation
	Updated      time.Time // updated every time this file is modified
	IsCompressed bool      // true when contents are compressed
	IsPurged     bool      // true when all contents except state files are removed
}

type Cwd string

func (cwd Cwd) StatePath() string {
	return cwd.String() + "/../." + path.Base(cwd.String()) + ".git-repo-mgr.state.json"
}

func (cwd Cwd) String() string {
	return string(cwd)
}

const GitTimeFormat = "Mon Jan _2 15:04:05 2006 -0700" // example: Wed Sep 25 15:30:25 2019 +0200

func (i *Instance) GitLastCommitTime(cwd Cwd) (time.Time, error) {
	res, err := i.RunGit(cwd, "log", "-1", "--format=%cd")
	if err != nil {
		return time.Time{}, err
	}
	t, err := time.Parse(GitTimeFormat, res)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}

func (i *Instance) GitOrigin(cwd Cwd) (string, error) {
	res, err := i.RunGit(cwd, "config", "--get", "remote.origin.url")
	if err != nil {
		return "", err
	}
	return res, nil
}

func (i *Instance) GitBranch(cwd Cwd) (string, error) {
	res, err := i.RunGit(cwd, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return res, nil
}

func (i *Instance) GitIsClean(cwd Cwd) (bool, error) {
	res, err := i.RunGit(cwd, "status")
	if err != nil {
		return false, err
	}
	return strings.Contains(res, "working tree clean"), nil
}

func (i *Instance) RunGit(cwd Cwd, firstArg string, arg ...string) (str string, err error) {
	// with first arg we make sure we don't by accident forget the argument(s)
	args := []string{firstArg}
	args = append(args, arg...)

	// throttle
	<- i.gitCmdConcurrencyThrottle
	defer func() {
		// panic dealing (to prevent concurrency locking up)
		if r := recover(); r != nil {
			log.Printf("recovered panic %s cwd %s args %+v", r, cwd, args)
			if err == nil {
				err = fmt.Errorf("%s", r)
			}
		}
		i.gitCmdConcurrencyThrottle <- true
	}()

	// prepare command
	cmd := exec.Command(GitBinaryPath, args...)
	cmd.Dir = cwd.String()

	// run
	stdoutStderr, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(stdoutStderr)), nil
}

const gitCmdConcurrencyLimit = 10

func New() *Instance {
	i := &Instance{
		gitCmdConcurrencyThrottle: make(chan bool, gitCmdConcurrencyLimit),
	}
	for n := 0 ; n < gitCmdConcurrencyLimit; n++ {
		i.gitCmdConcurrencyThrottle <- true
	}
	return i
}
