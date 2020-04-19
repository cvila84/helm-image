package main

import (
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"os"
)

var version = "SNAPSHOT"

func main() {
	ref, err := name.ParseReference("gcr.io/google-containers/pause")
	if err != nil {
		panic(err)
	}

	img, err := remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		panic(err)
	}
	f, err := os.Create("output.tar")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	tarball.MultiWriteToFile()
	if err := tarball.Write(ref, img, f); err != nil {
		panic(err)
	}
	//cmd := cmd.NewRootCmd(os.Stdout, os.Args[1:])
	//if err := cmd.Execute(); err != nil {
	//	os.Exit(1)
	//}
}
