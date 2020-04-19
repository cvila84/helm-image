module github.com/gemalto/helm-image

go 1.13

require (
	github.com/Microsoft/hcsshim v0.8.7 // indirect
	github.com/containerd/containerd v1.3.3
	github.com/containerd/continuity v0.0.0-20200228182428-0f16d7a0959c // indirect
	github.com/containerd/ttrpc v1.0.0 // indirect
	github.com/containerd/typeurl v1.0.0 // indirect
	github.com/docker/go-events v0.0.0-20190806004212-e31b211e4f1c // indirect
	github.com/gogo/googleapis v1.3.2 // indirect
	github.com/google/go-containerregistry v0.0.0-20200320200342-35f57d7d4930
	github.com/opencontainers/runc v0.1.1 // indirect
	github.com/opencontainers/runtime-spec v1.0.2 // indirect
	github.com/spf13/cobra v0.0.6
	github.com/syndtr/gocapability v0.0.0-20180916011248-d98352740cb2 // indirect
)

replace github.com/docker/distribution v2.7.1+incompatible => github.com/docker/distribution v2.7.1-0.20190205005809-0d3efadf0154+incompatible
