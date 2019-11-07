package service

import (
	"fmt"
	"io/ioutil"
	"log"
)

// compress
// remove & clone
// metadata file
// conf for exclusions / whitelist
// exclude ourselves

type Instance struct {

}

func (i *Instance) Run() error {
	files, err := ioutil.ReadDir("./")
	if err != nil {
		log.Fatal(err)
	}

	for _, f := range files {
		fmt.Println(f.Name())
	}

	return nil
}

func New() *Instance {
	return &Instance{}
}