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

	"github.com/satori/go.uuid"

	"golang.org/x/net/context"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/cloudbuild/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/storage/v1"
	cstorage "google.golang.org/cloud/storage"
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
	buildObject := fmt.Sprintf("build/%s-%s.tar.gz", *name, uuid.NewV4())

	ctx := context.Background()
	hc, err := google.DefaultClient(ctx, storage.CloudPlatformScope)
	if err != nil {
		log.Fatalf("Could not get authenticated HTTP client: %v", err)
	}

	/*
		plusClient, err := plus.New(hc)
		if err != nil {
			log.Fatalf("Could not get plus client: %v", err)
		}
		profile, err := plusClient.People.Get("me").Do()
		if err != nil {
			log.Fatalf("Could not get current user: %v", err)
		}
		log.Printf("Deploying as %v", profile.Id)
	*/

	if err := setupBucket(ctx, hc, stagingBucket); err != nil {
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
		log.Fatalf("Could not create build: %v", err)
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

// HACK: workaround for lack of type for "Metadata" field.
func getBuildID(op *cloudbuild.Operation) (string, error) {
	if op.Metadata == nil {
		return "", errors.New("missing Metadata in operation")
	}
	if m, ok := op.Metadata.(map[string]interface{}); ok {
		b, err := json.Marshal(m["build"])
		if err != nil {
			return "", err
		}
		build := &cloudbuild.Build{}
		if err := json.Unmarshal(b, &build); err != nil {
			return "", err
		}
		return build.Id, nil
	}
	return "", errors.New("unknown type for op")
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
		if err := tw.WriteHeader(hdr); err != nil {
			return err
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
