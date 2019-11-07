package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"sync"
)

// compress
// remove & clone
// metadata file
// conf for exclusions / whitelist
// exclude ourselves

type Instance struct {
	conf *Conf
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

	// @todo check stale git

	// @todo check last used

	log.Printf("%s %v %v", path, gitPathF, gitErr)

	return nil
}

func New() *Instance {
	return &Instance{}
}
