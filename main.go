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

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil || !os.IsNotExist(err)
}

func GetImageID(imageName string) (string, error) {
	// Create a new Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return "", err
	}
	defer cli.Close()

	imageInspect, err := cli.ImageInspect(context.Background(), imageName)
	if err != nil {
		return "", err
	}

	return imageInspect.ID, nil
}

func loadProject(ctx context.Context, projectName string, workingDir string) (*types.Project, error) {
	configFile := filepath.Join(workingDir, "docker-compose.yml")
	if !fileExists(configFile) {
		configFile = filepath.Join(workingDir, "docker-compose.yaml")
	}

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

func main() {
	ctx := context.Background()

	dockerCli, err := command.NewDockerCli()
	if err != nil {
		quit(err)
	}
	if err := dockerCli.Initialize(flags.NewClientOptions()); err != nil {
		quit(err)
	}

	backend := compose.NewComposeService(dockerCli)
	sv, err := backend.List(ctx, api.ListOptions{All: true})
	if err != nil {
		quit(err)
	}

	for _, view := range sv {
		projectName := view.Name
		if !strings.HasPrefix(view.Status, "running(") {
			fmt.Fprintln(os.Stderr, "skipping:", projectName, view.Status)
			continue
		}
		images, err := backend.Images(ctx, projectName, api.ImagesOptions{})
		if err != nil {
			quit(err)
		}

		project, err := loadProject(ctx, projectName, filepath.Dir(view.ConfigFiles))
		if err != nil {
			fmt.Println(project, view.ConfigFiles, filepath.Dir(view.ConfigFiles))
			quit(err)
		}

		needsRestart := false
		for _, image := range images {
			fmt.Println(view.Name, image.Repository, image.ID)
			err := backend.Pull(ctx, project, api.PullOptions{
				Quiet:           false,
				IgnoreFailures:  false,
				IgnoreBuildable: true,
			})
			if err != nil {
				quit(err)
			}

			imageWithTag := fmt.Sprintf("%s:%s", image.Repository, image.Tag)
			currentImageID, err := GetImageID(imageWithTag)
			if err != nil {
				quit(err)
			}

			if currentImageID != image.ID {
				fmt.Println(image.Repository, "has been updated")
				needsRestart = true
			}
		}

		if needsRestart {
			upOpts := api.UpOptions{
				Create: api.CreateOptions{
					Build:         &api.BuildOptions{},
					RemoveOrphans: true,
					Recreate:      api.RecreateForce,
				},
				Start: api.StartOptions{
					Project:  project,
					Services: project.ServiceNames(),
				},
			}

			fmt.Println("Restarting project:", projectName)
			if err := backend.Up(ctx, project, upOpts); err != nil {
				quit(err)
			}
		}
	}
}
