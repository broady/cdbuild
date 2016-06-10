# cdbuild

*Cloud Docker Build*

Build docker images remotely and push to Google Cloud Container Repository (gcr.io).

## Install

First, install the [Cloud SDK](https://cloud.google.com/sdk/).

Then, install `cdbuild`

    $ go get -u github.com/broady/cdbuild

## Usage

    $ gcloud auth login 

    $ ls 
    Dockerfile
    main.go

    $ cdbuild -project $MYPROJECT -name $IMAGENAME

The image is now available at `gcr.io/$MYPROJECT/$IMAGENAME`.

You can optionally add a version (Docker calls these tags) for the image by appending `:$VERSION`. For example:

    $ cdbuild -project $MYPROJECT -name $IMAGENAME:v1

## Support

This is not an official Google product, just an experiment.

## License

See [LICENSE](LICENSE).
