package main

import ("github.com/RobinUS2/git-repo-mgr/service"
	"log"
)

func main() {
	i := service.New()
	if err := i.Run(); err != nil {
		log.Fatalf("failed: %s", err)
	}
}
