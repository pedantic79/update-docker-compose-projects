package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/client"
	"github.com/pedantic79/update-docker-compose-projects/dockercli"
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

	dockerCli := unwrap(utils.NewDockerCli())
	projectViews := unwrap(dockercompose.GetList(ctx, dockerCli, api.ListOptions{All: true}))

	for _, projectView := range projectViews {
		fmt.Printf("Name:%s, Status:%s ConfigFile:%s\n", projectView.Name, projectView.Status, projectView.ConfigFiles)
		projectName := projectView.Name

		// only include running projects
		if !strings.HasPrefix(projectView.Status, "running(") {
			fmt.Fprintln(os.Stderr, "skipping:", projectName, projectView.Status)
			continue
		}

		project := unwrap(dockercompose.LoadProject(ctx, dockerCli, projectView))
		images := unwrap(dockercompose.GetImages(ctx, dockerCli, projectName, api.ImagesOptions{}))

		// Do a pull on the images
		err := dockercompose.UpdateImages(ctx, dockerCli, project)
		if err != nil {
			panic(err)
		}

		needsRestart := unwrap(dockercli.NeedsRestart(ctx, dockerClient, images))
		// If any of the images have been updated, then restart the project
		if needsRestart {
			err := dockercompose.Restart(ctx, dockerCli, project)
			if err != nil {
				panic(err)
			}
			_ = unwrap(dockercli.ImagePrune(ctx, dockerClient))
		}
	}
}
