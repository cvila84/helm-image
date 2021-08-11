module github.com/gemalto/helm-image

go 1.15

require (
	github.com/containerd/console v1.0.2
	github.com/containerd/containerd v1.5.4
	github.com/docker/distribution v2.7.1+incompatible
	github.com/opencontainers/go-digest v1.0.0
	github.com/opencontainers/image-spec v1.0.1
	github.com/opencontainers/runtime-spec v1.0.3-0.20210326190908-1c3f411f0417 // indirect
	github.com/spf13/cobra v1.1.3
	golang.org/x/sys v0.0.0-20210426230700-d19ff857e887 // indirect
	helm.sh/helm/v3 v3.6.1
	k8s.io/api v0.21.0
	k8s.io/client-go v0.21.0
	rsc.io/letsencrypt v0.0.3 // indirect
)

replace github.com/docker/distribution v2.7.1+incompatible => github.com/docker/distribution v2.7.1-0.20190205005809-0d3efadf0154+incompatible
