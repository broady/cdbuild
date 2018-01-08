// Copyright 2016 Google Inc. All rights reserved.
// Use of this source code is governed by the Apache 2.0
// license that can be found in the LICENSE file.

// Command cdbuild uses Google Cloud Container Builder to build a docker image.
package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	uuid "github.com/satori/go.uuid"

	cstorage "cloud.google.com/go/storage"
	"golang.org/x/net/context"
	"golang.org/x/oauth2/google"
	cloudbuild "google.golang.org/api/cloudbuild/v1"
	"google.golang.org/api/googleapi"
	storage "google.golang.org/api/storage/v1"
)

var (
	projectID = flag.String("project", "", "Project ID. Required.")
	name      = flag.String("name", "", "Image name. Required.")
)

func main() {
	flag.Parse()
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
	}
	if *projectID == "" {
		fmt.Fprintln(os.Stderr, "Missing 'project' flag.")
		flag.Usage()
		os.Exit(2)
	}
	if *name == "" {
		fmt.Fprintln(os.Stderr, "Missing 'name' flag.")
		flag.Usage()
		os.Exit(2)
	}

	stagingBucket := "cdbuild-" + *projectID
	buildObject := fmt.Sprintf("build/%s-%s.tar.gz", *name, uuid.Must(uuid.NewV4()))

	ctx := context.Background()
	hc, err := google.DefaultClient(ctx, storage.CloudPlatformScope)
	if err != nil {
		log.Fatalf("Could not get authenticated HTTP client: %v", err)
	}

	if err := setupBucket(ctx, hc, stagingBucket); err != nil {
		if gerr, ok := err.(*googleapi.Error); ok {
			if gerr.Code == 403 {
				// HACK(cbro): storage returns a 403 if billing is not enabled.
				log.Fatalf("Could not set up Cloud Storage bucket. It's possible billing is not enabled. Root cause: %v", err)
			}
		}
		log.Fatalf("Could not set up buckets: %v", err)
	}

	log.Printf("Pushing code to gs://%s/%s", stagingBucket, buildObject)

	if err := uploadTar(ctx, hc, stagingBucket, buildObject); err != nil {
		log.Fatalf("Could not upload source: %v", err)
	}

	api, err := cloudbuild.New(hc)
	if err != nil {
		log.Fatalf("Could not get cloudbuild client: %v", err)
	}
	call := api.Projects.Builds.Create(*projectID, &cloudbuild.Build{
		LogsBucket: stagingBucket,
		Source: &cloudbuild.Source{
			StorageSource: &cloudbuild.StorageSource{
				Bucket: stagingBucket,
				Object: buildObject,
			},
		},
		Steps: []*cloudbuild.BuildStep{
			{
				Name: "gcr.io/cloud-builders/dockerizer",
				Args: []string{"gcr.io/" + *projectID + "/" + *name},
			},
		},
		Images: []string{"gcr.io/" + *projectID + "/" + *name},
	})
	op, err := call.Context(ctx).Do()
	if err != nil {
		if gerr, ok := err.(*googleapi.Error); ok {
			if gerr.Code == 404 {
				// HACK(cbro): the API does not return a good error if the API is not enabled.
				fmt.Fprintln(os.Stderr, "Could not create build. It's likely the Cloud Container Builder API is not enabled.")
				fmt.Fprintf(os.Stderr, "Go here to enable it: https://console.cloud.google.com/apis/api/cloudbuild.googleapis.com/overview?project=%s\n", *projectID)
				os.Exit(1)
			}
		}
		log.Fatalf("Could not create build: %#v", err)
	}
	remoteID, err := getBuildID(op)
	if err != nil {
		log.Fatalf("Could not get build ID from op: %v", err)
	}

	log.Printf("Logs at https://console.cloud.google.com/m/cloudstorage/b/%s/o/log-%s.txt", stagingBucket, remoteID)

	for {
		b, err := api.Projects.Builds.Get(*projectID, remoteID).Do()
		if err != nil {
			log.Fatalf("Could not get build status: %v", err)
		}

		if b.Status != "WORKING" && b.Status != "QUEUED" {
			log.Printf("Build status: %v", b.Status)
			break
		}

		time.Sleep(time.Second)
	}

	c, err := cstorage.NewClient(ctx)
	if err != nil {
		log.Fatalf("Could not make Cloud storage client: %v", err)
	}
	defer c.Close()
	if err := c.Bucket(stagingBucket).Object(buildObject).Delete(ctx); err != nil {
		log.Fatalf("Could not delete source tar.gz: %v", err)
	}
	log.Print("Cleaned up.")
}

func getBuildID(op *cloudbuild.Operation) (string, error) {
	if len(op.Metadata) == 0 {
		return "", errors.New("missing Metadata in operation")
	}
	build := &cloudbuild.Build{}
	if err := json.Unmarshal(op.Metadata, &build); err != nil {
		return "", err
	}
	return build.Id, nil
}

func setupBucket(ctx context.Context, hc *http.Client, bucket string) error {
	s, err := storage.New(hc)
	if err != nil {
		return err
	}
	if _, err := s.Buckets.Get(bucket).Do(); err != nil {
		if gerr, ok := err.(*googleapi.Error); ok {
			if gerr.Code != 404 {
				return err
			}
		} else {
			return err
		}
	} else {
		return nil
	}
	_, err = s.Buckets.Insert(*projectID, &storage.Bucket{
		Name: bucket,
	}).Do()
	return err
}

func uploadTar(ctx context.Context, hc *http.Client, bucket string, objectName string) error {
	c, err := cstorage.NewClient(ctx)
	if err != nil {
		return err
	}
	defer c.Close()

	w := c.Bucket(bucket).Object(objectName).NewWriter(ctx)
	gzw := gzip.NewWriter(w)
	tw := tar.NewWriter(gzw)

	if err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if path == "." {
			return nil
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = path
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	}); err != nil {
		w.CloseWithError(err)
		return err
	}
	if err := tw.Close(); err != nil {
		w.CloseWithError(err)
		return err
	}
	if err := gzw.Close(); err != nil {
		w.CloseWithError(err)
		return err
	}
	return w.Close()
}
