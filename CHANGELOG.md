# Release Notes

## Version 1.0.7 - 04/04/2022
* Rely on credential manager (only windows) or docker configuration to get repository authentication information
* Bump to containerd 1.6.2

## Version 1.0.6 - 08/11/2021
* Added -o flag to save command (set the output file name)

## Version 1.0.5 - 04/06/2021
* Support cron jobs [#7](https://github.com/cvila84/helm-image/issues/7)

## Version 1.0.4 - 02/02/2021
* Added verbose traces (as chart rendering process may be long for big umbrella charts)

## Version 1.0.3 - 01/25/2021
* Added pull command (calls docker pull for each image retrieved from the chart)

## Version 1.0.2 - 09/17/2020
* Fix issue on helm call when present in PATH

## Version 1.0.1 - 06/26/2020
* Added cache command to list and clean the containerd docker images cache
* Generated TAR only contains the extracted images for the chart, instead of all the images contained in the cache

## Version 1.0 - 06/20/2020
* First delivery on Github.
