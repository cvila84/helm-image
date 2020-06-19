# Helm Image

[![Build Status](https://travis-ci.com/cvila84/helm-image.svg?branch=master)](https://travis-ci.com/cvila84/helm-image)
![helm-image](https://cvila84.github.io/helm-image/assets/logo.png)

## What is it ?

This is a helm plugin to list or even store in an archive the docker images that are referenced from a given chart and values

## When do I need this ?

If you plan to install charts in an environment without internet (such as a VM created and run in a secured cloud space), you would like to know at environment creation time what are the docker images that will be pulled later on during chart installation 

## Usage

To list the docker images referenced by a chart :
```
-bash-4.2$ helm image list prometheus-operator-0.20.7.tgz
docker.io/bitnami/kube-state-metrics:1.9.7-debian-10-r13
docker.io/bitnami/prometheus-operator:0.40.0-debian-10-r0
```

To save these docker images in a TAR :
```
-bash-4.2$ helm image save prometheus-operator-0.20.7.tgz
Pulling image docker.io/bitnami/prometheus-operator:0.40.0-debian-10-r0...
Successfully pulled docker.io/bitnami/prometheus-operator:0.40.0-debian-10-r0 image
Pulling image docker.io/bitnami/kube-state-metrics:1.9.7-debian-10-r13...
Successfully pulled docker.io/bitnami/kube-state-metrics:1.9.7-debian-10-r13 image
Saving all images in prometheus-operator.tar...
Successfully saved all images in prometheus-operator.tar
```

## How does it work ?

To list the images, a dry-run helm installation is actually performed, then all generated manifests are parsed in a temporary directory to find all container templates for all deployments and statefulsets

To save the images, a containerd server is launched in background, with a client pulling all the images, then exporting them in a file

## Known bugs and limitations

This plugin has only been tested on Windows so far

 