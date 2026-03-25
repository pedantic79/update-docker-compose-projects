package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/docker/compose/v5/pkg/api"
	"github.com/pedantic79/update-docker-compose-projects/dockerclient"
)

func unwrap[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func main() {
	ctx := context.Background()
	client := unwrap(dockerclient.New())
	defer client.Close()

	needsPrune := false
	projectViews := unwrap(client.ServiceList(ctx, api.ListOptions{All: true}))
	for _, projectView := range projectViews {
		projectName := projectView.Name
		fmt.Printf("Name:%s, Status:%s ConfigFile:%s\n", projectName, projectView.Status, projectView.ConfigFiles)

		// only include running projects
		if !strings.HasPrefix(projectView.Status, "running(") {
			fmt.Fprintln(os.Stderr, "skipping:", projectName, projectView.Status)
			continue
		}

		// get project from projectView
		// Get original image summary
		// Do a pull on the images
		project := unwrap(client.ServiceLoadProject(ctx, projectView))
		images := unwrap(client.ServiceImages(ctx, projectName, api.ImagesOptions{}))
		unwrap(client.ServicePull(ctx, project))

		// If any of the images have been updated, then restart the project
		needsRestart := unwrap(client.NeedsRestart(ctx, images))
		if needsRestart {
			unwrap(client.ServiceUp(ctx, project))
			needsPrune = true
		}
	}

	if needsPrune {
		fmt.Println("Pruning images...")
		_ = unwrap(client.ImagePrune(ctx))
	}
}
