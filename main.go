package main

import ("github.com/RobinUS2/git-repo-mgr/service"
	"log"
)

func main() {
	i := service.New()
	log.Printf("%+v", i)
}
