module github.com/gemalto/helm-image

go 1.15

require (
	github.com/containerd/cgroups v0.0.0-20210114181951-8a68de567b68 // indirect
	github.com/containerd/console v0.0.0-20180822173158-c12b1e7919c1
	github.com/containerd/containerd v1.4.13
	github.com/docker/distribution v2.7.1+incompatible
	github.com/docker/go-events v0.0.0-20190806004212-e31b211e4f1c // indirect
	github.com/gogo/googleapis v1.4.0 // indirect
	github.com/opencontainers/go-digest v1.0.0
	github.com/opencontainers/image-spec v1.0.1
	github.com/opencontainers/selinux v1.8.0 // indirect
	github.com/spf13/cobra v1.1.1
	helm.sh/helm/v3 v3.5.1
	k8s.io/api v0.20.1
	k8s.io/client-go v0.20.1
	rsc.io/letsencrypt v0.0.3 // indirect
)

replace github.com/docker/distribution v2.7.1+incompatible => github.com/docker/distribution v2.7.1-0.20190205005809-0d3efadf0154+incompatible
