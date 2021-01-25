package docker

import (
	"log"
	"os"
	"os/exec"
)

func Pull(image string, debug bool) error {
	dockerPath := "docker"
	var myargs = []string{"pull", image}
	if debug {
		log.Printf("Running %s %v\n", dockerPath, myargs)
	}
	cmd := exec.Command(dockerPath, myargs...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	err := cmd.Run()
	if err != nil {
		return err
	}
	return nil
}
