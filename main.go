package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/client"
	"github.com/pedantic79/update-docker-compose-projects/dockerclient"
	"github.com/pedantic79/update-docker-compose-projects/dockercompose"
	"github.com/pedantic79/update-docker-compose-projects/utils"
)

func unwrap[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func main() {
	ctx := context.Background()

	// Create a new Docker client
	dockerClient := unwrap(client.New(client.FromEnv, client.WithAPIVersionFromEnv()))
	defer dockerClient.Close()

	// Create a new Docker cli
	dockerCli := unwrap(utils.NewDockerCli())
	projectViews := unwrap(dockercompose.ServiceList(ctx, dockerCli, api.ListOptions{All: true}))

	for _, projectView := range projectViews {
		projectName := projectView.Name
		fmt.Printf("Name:%s, Status:%s ConfigFile:%s\n", projectName, projectView.Status, projectView.ConfigFiles)

		// only include running projects
		if !strings.HasPrefix(projectView.Status, "running(") {
			fmt.Fprintln(os.Stderr, "skipping:", projectName, projectView.Status)
			continue
		}

		// get project from projectView
		project := unwrap(dockercompose.ServiceLoadProject(ctx, dockerCli, projectView))

		// Get original image summary
		images := unwrap(dockercompose.ServiceImages(ctx, dockerCli, projectName, api.ImagesOptions{}))

		// Do a pull on the images
		err := dockercompose.ServicePull(ctx, dockerCli, project)
		if err != nil {
			panic(err)
		}

		// If any of the images have been updated, then restart the project
		needsRestart := unwrap(dockerclient.NeedsRestart(ctx, dockerClient, images))
		if needsRestart {
			err := dockercompose.ServiceUp(ctx, dockerCli, project)
			if err != nil {
				panic(err)
			}
			_ = unwrap(dockerclient.ImagePrune(ctx, dockerClient))
		}
	}
}
