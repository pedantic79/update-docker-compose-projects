package dockerclient

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v5/cmd/display"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/compose/v5/pkg/compose"
	"github.com/fatih/color"
	"github.com/moby/moby/client"
)

var (
	orange       = color.RGB(255, 128, 0)
	orangeString = orange.SprintfFunc()
)

type DockerClient struct {
	client    *client.Client
	dockerCli *command.DockerCli
}

func New() (*DockerClient, error) {
	dockerClient, err := client.New(client.FromEnv, client.WithAPIVersionFromEnv())
	if err != nil {
		return nil, err
	}

	dockerCli, err := command.NewDockerCli()
	if err != nil {
		return nil, err
	}

	if err := dockerCli.Initialize(flags.NewClientOptions()); err != nil {
		return nil, err
	}

	return &DockerClient{
		client:    dockerClient,
		dockerCli: dockerCli,
	}, nil
}

func (d *DockerClient) Close() error {
	return d.client.Close()
}

func (d *DockerClient) newDockerComposeService() (api.Compose, error) {
	ttyDisplay := display.Full(d.dockerCli.Err(), d.dockerCli.Out(), false)

	return compose.NewComposeService(d.dockerCli, compose.WithEventProcessor(ttyDisplay))
}

func (d *DockerClient) NeedsRestart(ctx context.Context, originalImages map[string]api.ImageSummary) (map[string][2]string, error) {
	logs := map[string][2]string{}

	// check if each image is updated
	for oImage := range maps.Values(originalImages) {

		// Check if the imageID has changed
		imageWithTag := fmt.Sprintf("%s:%s", oImage.Repository, oImage.Tag)
		currentImage, err := d.client.ImageInspect(ctx, imageWithTag)
		if err != nil {
			return map[string][2]string{}, err
		}

		if currentImage.ID != oImage.ID {
			logs[imageWithTag] = [2]string{oImage.ID, currentImage.ID}
		}
	}

	return logs, nil
}

func (d *DockerClient) ImagePrune(ctx context.Context) (client.ImagePruneResult, error) {
	fmt.Println(color.RedString("Pruning images..."))
	return d.client.ImagePrune(ctx, client.ImagePruneOptions{})
}

func (d *DockerClient) ServiceList(ctx context.Context, listOpts api.ListOptions) ([]api.Stack, error) {
	service, err := d.newDockerComposeService()
	if err != nil {
		return nil, err
	}

	return service.List(ctx, listOpts)
}

func (d *DockerClient) ServiceImages(ctx context.Context, projectName string, imagesOpts api.ImagesOptions) (map[string]api.ImageSummary, error) {
	service, err := d.newDockerComposeService()
	if err != nil {
		return nil, err
	}

	return service.Images(ctx, projectName, imagesOpts)
}

// ServiceUp is trying to replicate this command
// docker compose up --force-recreate --build --remove-orphans --pull always -d
// we're ignoring the pull here, because we've already pulled images previously
func (d *DockerClient) ServiceUp(ctx context.Context, project *types.Project, needsRestart map[string][2]string) (any, error) {
	for image, ids := range needsRestart {
		fmt.Printf("Restarting %s[%s]\nOld: %s\nNew: %s\n",
			color.RedString(project.Name),
			color.BlueString(image),
			orangeString(ids[0]),
			orangeString(ids[1]),
		)
	}

	// Create a new Compose service instance
	service, err := d.newDockerComposeService()
	if err != nil {
		return nil, err
	}

	services := project.ServiceNames()
	create := api.CreateOptions{
		Build: &api.BuildOptions{
			Pull:     false,
			Services: services,
		},
		Services:             services,
		RemoveOrphans:        true,
		IgnoreOrphans:        false,
		Recreate:             api.RecreateForce,
		RecreateDependencies: api.RecreateDiverged,
		Inherit:              true,
		Timeout:              nil,
		QuietPull:            false,
	}

	// Start the services defined in the Compose file
	return nil, service.Up(ctx, project, api.UpOptions{
		Create: create,
		Start: api.StartOptions{
			Project:        project,
			Attach:         nil,
			AttachTo:       []string{},
			OnExit:         0,
			ExitCodeFrom:   "",
			Wait:           false,
			WaitTimeout:    0,
			Services:       services,
			Watch:          false,
			NavigationMenu: false,
		},
	})
}

func (d *DockerClient) ServicePull(ctx context.Context, project *types.Project) (any, error) {
	service, err := d.newDockerComposeService()
	if err != nil {
		return nil, err
	}

	return nil, service.Pull(ctx, project, api.PullOptions{
		Quiet:           false,
		IgnoreFailures:  false,
		IgnoreBuildable: true,
	})
}

func (d *DockerClient) ServiceLoadProject(ctx context.Context, stack api.Stack) (*types.Project, error) {
	configFile := stack.ConfigFiles
	workingDir := filepath.Dir(configFile)
	service, err := d.newDockerComposeService()
	if err != nil {
		return nil, err
	}

	project, err := service.LoadProject(ctx, api.ProjectLoadOptions{
		WorkingDir:  workingDir,
		ConfigPaths: []string{configFile},
	})
	if err != nil {
		return nil, err
	}

	return project, nil
}
