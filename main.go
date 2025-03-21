package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v2/pkg/api"
	"github.com/docker/compose/v2/pkg/compose"
	"github.com/docker/docker/client"
)

func quit(err error) {
	// fmt.Fprintln(os.Stderr, err)
	// os.Exit(1)
	panic(err)
}

// GetImageID returns the ImageID via the docker API. Pass it the full image with tag
func GetImageID(cli *client.Client, imageName string) (string, error) {
	imageInspect, err := cli.ImageInspect(context.Background(), imageName)
	if err != nil {
		return "", err
	}

	return imageInspect.ID, nil
}

func loadProject(ctx context.Context, projectName string, configFile string) (*types.Project, error) {
	workingDir := filepath.Dir(configFile)

	options := &cli.ProjectOptions{
		WorkingDir:  workingDir,
		ConfigPaths: []string{configFile},
		Name:        projectName,
	}

	project, err := options.LoadProject(ctx)
	if err != nil {
		return nil, err
	}

	return project, nil
}

// Wrap creation of compse service
func newDockerComposeService() api.Service {
	dockerCli, err := command.NewDockerCli()
	if err != nil {
		quit(err)
	}

	if err := dockerCli.Initialize(flags.NewClientOptions()); err != nil {
		quit(err)
	}

	return compose.NewComposeService(dockerCli)
}

func main() {
	ctx := context.Background()

	// Create a new Docker client
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		quit(err)
	}
	defer dockerClient.Close()

	service := newDockerComposeService()
	sv, err := service.List(ctx, api.ListOptions{All: true})
	if err != nil {
		quit(err)
	}

	for _, view := range sv {
		projectName := view.Name

		// only include running projects
		if !strings.HasPrefix(view.Status, "running(") {
			fmt.Fprintln(os.Stderr, "skipping:", projectName, view.Status)
			continue
		}

		// load the project information for future commands
		project, err := loadProject(ctx, projectName, view.ConfigFiles)
		if err != nil {
			fmt.Println(project, view.ConfigFiles, filepath.Dir(view.ConfigFiles))
			quit(err)
		}

		// get Images for the project
		images, err := service.Images(ctx, projectName, api.ImagesOptions{})
		if err != nil {
			quit(err)
		}

		// Loop through the images
		needsRestart := false
		for i := range images {
			image := &images[i]
			fmt.Println(view.Name, image.Repository, image.ID)

			// Attempt a image pull on the image
			err := service.Pull(ctx, project, api.PullOptions{
				Quiet:           false,
				IgnoreFailures:  false,
				IgnoreBuildable: true,
			})
			if err != nil {
				quit(err)
			}

			// Check if the imageID has changed
			imageWithTag := fmt.Sprintf("%s:%s", image.Repository, image.Tag)
			currentImageID, err := GetImageID(dockerClient, imageWithTag)
			if err != nil {
				quit(err)
			}

			if currentImageID != image.ID {
				fmt.Println(image.Repository, "has been updated")
				needsRestart = true
			}
		}

		// If any of the images have been updated, then restart the project
		if needsRestart {
			// We're trying to replicate this command
			// docker compose up --force-recreate --build --remove-orphans --pull always -d
			// we're ignoring the pull here, because we've already pulled images previously
			upOpts := api.UpOptions{
				Create: api.CreateOptions{
					Build:         &api.BuildOptions{},
					RemoveOrphans: true,
					Recreate:      api.RecreateForce,
				},
				// this specifies which services to start, we always want all of them
				Start: api.StartOptions{
					Project:  project,
					Services: project.ServiceNames(),
				},
			}

			fmt.Println("Restarting project:", projectName)
			if err := service.Up(ctx, project, upOpts); err != nil {
				quit(err)
			}
		}
	}
}
