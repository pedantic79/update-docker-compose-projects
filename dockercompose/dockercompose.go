package dockercompose

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/cli/cli/command"
	"github.com/docker/compose/v5/cmd/display"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/compose/v5/pkg/compose"
	"github.com/pedantic79/update-docker-compose-projects/utils"
)

// Wrap creation of compse service
func newDockerComposeService(dockerCli *command.DockerCli) (api.Compose, error) {
	if dockerCli == nil {
		dcli, err := utils.NewDockerCli()
		if err != nil {
			return nil, err
		}
		dockerCli = dcli
	}

	ttyDisplay := display.Full(dockerCli.Err(), dockerCli.Out(), false)

	return compose.NewComposeService(dockerCli, compose.WithEventProcessor(ttyDisplay))
}

// Restart is trying to replicate this command
// docker compose up --force-recreate --build --remove-orphans --pull always -d
// we're ignoring the pull here, because we've already pulled images previously
func Restart(ctx context.Context, dockerCli *command.DockerCli, project *types.Project) error {
	fmt.Println("Restarting", project.Name)

	// Create a new Compose service instance
	service, err := newDockerComposeService(dockerCli)
	if err != nil {
		return err
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
	return service.Up(ctx, project, api.UpOptions{
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

func GetList(ctx context.Context, dockerCli *command.DockerCli, listOpts api.ListOptions) ([]api.Stack, error) {
	// Create a new Compose service instance
	service, err := newDockerComposeService(dockerCli)
	if err != nil {
		return nil, err
	}

	return service.List(ctx, listOpts)
}

func GetImages(ctx context.Context, dockerCli *command.DockerCli, projectName string, imagesOpts api.ImagesOptions) (map[string]api.ImageSummary, error) {
	// Create a new Compose service instance
	service, err := newDockerComposeService(dockerCli)
	if err != nil {
		return nil, err
	}

	return service.Images(ctx, projectName, imagesOpts)
}

func UpdateImages(ctx context.Context, dockerCli *command.DockerCli, project *types.Project) error {
	service, err := newDockerComposeService(dockerCli)
	if err != nil {
		return err
	}

	return service.Pull(ctx, project, api.PullOptions{
		Quiet:           false,
		IgnoreFailures:  false,
		IgnoreBuildable: true,
	})
}

func LoadProject(ctx context.Context, dockerCli *command.DockerCli, stack api.Stack) (*types.Project, error) {
	configFile := stack.ConfigFiles
	workingDir := filepath.Dir(configFile)

	options := api.ProjectLoadOptions{
		WorkingDir:  workingDir,
		ConfigPaths: []string{configFile},
	}

	service, err := newDockerComposeService(dockerCli)
	if err != nil {
		return nil, err
	}

	project, err := service.LoadProject(ctx, options)
	if err != nil {
		return nil, err
	}

	return project, nil
}
