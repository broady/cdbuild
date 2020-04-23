*** DEFUNCT: use `gcloud builds submit` instead ***

NOTE: This repo is still serves as a useful demonstration on how to use `google.golang.org/api/cloudbuild/v1`.

# cdbuild

[![Build Status](https://travis-ci.org/broady/cdbuild.svg?branch=master)](https://travis-ci.org/broady/cdbuild)

*Cloud Docker Build*

Build docker images remotely and push to Google Cloud Container Repository (gcr.io).

## Install

First, install the [Cloud SDK](https://cloud.google.com/sdk/).

Then, install `cdbuild`

    $ go get -u github.com/broady/cdbuild

## Usage

    $ gcloud beta auth application-default login

    $ ls 
    Dockerfile
    main.go

    $ cdbuild -project $MYPROJECT -name $IMAGENAME

The image is now available at `gcr.io/$MYPROJECT/$IMAGENAME`.

You can optionally add a version (Docker calls these tags) for the image by appending `:$VERSION`. For example:

    $ cdbuild -project $MYPROJECT -name $IMAGENAME:v1

## Run the example

    $ cd $GOPATH/src/github.com/broady/cdbuild/example

    $ cdbuild -project $MYPROJECT -name cdbuild-example
    2016/06/10 12:02:11 Pushing code to gs://cdbuild-$MYPROJECT/build/cdbuild-example-43e1d708-0490-4b26-b7e2-cebfefaf9be9.tar.gz
    2016/06/10 12:02:13 Logs at https://console.cloud.google.com/m/cloudstorage/b/cdbuild-$MYPROJECT/o/log-e30edc79-2986-425a-be6d-9f66b3772546.txt
    2016/06/10 12:03:09 Build status: SUCCESS
    2016/06/10 12:03:09 Cleaned up.

    $ gcloud docker run gcr.io/$MYPROJECT/cdbuild-example
    Unable to find image 'gcr.io/$MYPROJECT/cdbuild-example:latest' locally
    latest: Pulling from $MYPROJECT/cdbuild-example

    9cb679a7b8e0: Pull complete
    b75784cf148d: Pull complete
    16c2272a37cb: Pull complete
    Digest: sha256:c7bf002a8df2cb21adddc10c089758bfc2fd34bf8d074b4245fecdc9662c09a3
    Status: Downloaded newer image for gcr.io/$MYPROJECT/cdbuild-example:latest
    + exec app
    2016/06/10 19:04:35 hello, world!

## Support

This is not an official Google product, just an experiment.

## License

See [LICENSE](LICENSE).
