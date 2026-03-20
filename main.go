package main

import (
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/compose/v5/pkg/compose"
	"github.com/moby/moby/client"
)

// GetImageID returns the ImageID via the docker API. Pass it the full image with tag
func GetImageID(ctx context.Context, cli *client.Client, imageName string) (string, error) {
	imageInspect, err := cli.ImageInspect(ctx, imageName)
	if err != nil {
		return "", err
	}

	return imageInspect.ID, nil
}

func ImagePrune(ctx context.Context, cli *client.Client) (client.ImagePruneResult, error) {
	fmt.Println("Pruning images...")
	return cli.ImagePrune(ctx, client.ImagePruneOptions{})
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
func newDockerComposeService() (api.Compose, error) {
	dockerCli, err := command.NewDockerCli()
	if err != nil {
		return nil, err
	}

	if err := dockerCli.Initialize(flags.NewClientOptions()); err != nil {
		return nil, err
	}

	return compose.NewComposeService(dockerCli)
}

func updateImages(ctx context.Context, dockerClient *client.Client, service api.Compose, images map[string]api.ImageSummary, project *types.Project) (bool, error) {
	// pull all images for project
	err := service.Pull(ctx, project, api.PullOptions{
		Quiet:           false,
		IgnoreFailures:  false,
		IgnoreBuildable: true,
	})
	if err != nil {
		return false, err
	}

	// check if each image is updated
	needsRestart := false
	for i := range maps.Keys(images) {
		image := images[i]

		// Check if the imageID has changed
		imageWithTag := fmt.Sprintf("%s:%s", image.Repository, image.Tag)
		currentImageID, err := GetImageID(ctx, dockerClient, imageWithTag)
		if err != nil {
			return false, err
		}

		if currentImageID != image.ID {
			fmt.Printf("%s has been updated: %s -> %s\n", image.Repository, image.ID, currentImageID)
			needsRestart = true
		}
	}
	return needsRestart, nil
}

// restartProject is trying to replicate this command
// docker compose up --force-recreate --build --remove-orphans --pull always -d
// we're ignoring the pull here, because we've already pulled images previously
// func restartProject(ctx context.Context, service api.Service, project *types.Project, projectName string) error {
// 	services := make([]string, 0, len(project.ServiceNames())+len(project.DisabledServiceNames()))
// 	services = append(services, project.ServiceNames()...)
// 	services = append(services, project.DisabledServiceNames()...)

// 	upOpts := api.UpOptions{
// 		Create: api.CreateOptions{
// 			Build:         &api.BuildOptions{},
// 			RemoveOrphans: true,
// 			Recreate:      api.RecreateForce,
// 			Services:      services,
// 		},
// 		// this specifies which services to start, we always want all of them
// 		Start: api.StartOptions{
// 			Project:  project,
// 			Services: services,
// 			Wait:     true,
// 		},
// 	}

// 	fmt.Println("Restarting project:", projectName, services)
// 	if err := service.Up(ctx, project, upOpts); err != nil {
// 		return err
// 	}

// 	return nil
// }

func runRestart(ctx context.Context, project *types.Project) error {
	cmd := exec.CommandContext(ctx, "docker", "compose", "up", "--force-recreate", "--build", "--remove-orphans", "-d")

	cmd.Dir = project.WorkingDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func main() {
	ctx := context.Background()

	// Create a new Docker client
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		panic(err)
	}
	defer dockerClient.Close()

	service, err := newDockerComposeService()
	if err != nil {
		panic(err)
	}

	projectViews, err := service.List(ctx, api.ListOptions{All: true})
	if err != nil {
		panic(err)
	}

	for _, projectView := range projectViews {
		projectName := projectView.Name

		// only include running projects
		if !strings.HasPrefix(projectView.Status, "running(") {
			fmt.Fprintln(os.Stderr, "skipping:", projectName, projectView.Status)
			continue
		}

		// load the project information for future commands
		project, err := loadProject(ctx, projectName, projectView.ConfigFiles)
		if err != nil {
			panic(fmt.Errorf("%v, %s: %w", project, projectView.ConfigFiles, err))
		}

		// get Images for the project
		images, err := service.Images(ctx, projectName, api.ImagesOptions{})
		if err != nil {
			panic(err)
		}

		// Do a pull on the images
		needsRestart, err := updateImages(ctx, dockerClient, service, images, project)
		if err != nil {
			panic(err)
		}

		// If any of the images have been updated, then restart the project
		if needsRestart {
			// if err := restartProject(ctx, service, project, projectName); err != nil {
			// 	panic(err)
			// }

			if err := runRestart(ctx, project); err != nil {
				panic(err)
			}

			if _, err := ImagePrune(ctx, dockerClient); err != nil {
				panic(err)
			}
		}
	}
}
