package docker

import (
	"context"
	"fmt"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v5/cmd/display"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/compose/v5/pkg/compose"
	"github.com/moby/moby/client"
	"github.com/pedantic79/update-docker-compose-projects/internal/updater"
)

type composeAPI interface {
	List(context.Context, api.ListOptions) ([]api.Stack, error)
	Ps(context.Context, string, api.PsOptions) ([]api.ContainerSummary, error)
	LoadProject(context.Context, api.ProjectLoadOptions) (*types.Project, error)
	Pull(context.Context, *types.Project, api.PullOptions) error
	Up(context.Context, *types.Project, api.UpOptions) error
}

type engineAPI interface {
	ImagePrune(context.Context, client.ImagePruneOptions) (client.ImagePruneResult, error)
	Close() error
}

type composeFactory func() (composeAPI, error)

// Backend adapts one Docker CLI context to the application's narrow updater
// boundary. Compose and Engine operations therefore always target the same
// daemon.
type Backend struct {
	compose           composeAPI
	engine            engineAPI
	context           string
	newProjectCompose composeFactory
}

func New() (*Backend, error) {
	dockerCLI, err := command.NewDockerCli()
	if err != nil {
		return nil, fmt.Errorf("create Docker CLI: %w", err)
	}

	clientOptions := flags.NewClientOptions()
	if err := dockerCLI.Initialize(clientOptions, command.WithInitializeClient(
		func(cli *command.DockerCli) (client.APIClient, error) {
			return command.NewAPIClientFromFlags(clientOptions, cli.ConfigFile())
		},
	)); err != nil {
		return nil, fmt.Errorf("initialize Docker CLI: %w", err)
	}

	engine := dockerCLI.Client()
	composeService, err := compose.NewComposeService(dockerCLI)
	if err != nil {
		_ = engine.Close()
		return nil, fmt.Errorf("create Compose service: %w", err)
	}

	return &Backend{
		compose: composeService,
		engine:  engine,
		context: dockerCLI.CurrentContext(),
		newProjectCompose: func() (composeAPI, error) {
			// Compose's full-screen event processor retains progress entries.
			// Give each project a fresh processor so pull and up stay together
			// without entries leaking into the next project.
			ttyDisplay := display.Full(dockerCLI.Err(), dockerCLI.Out(), false)
			return compose.NewComposeService(
				dockerCLI,
				compose.WithEventProcessor(ttyDisplay),
			)
		},
	}, nil
}

func (b *Backend) Close() error {
	return b.engine.Close()
}

func (b *Backend) Context() string {
	return b.context
}

func (b *Backend) LoadProject(ctx context.Context, ref updater.ProjectRef) (*types.Project, error) {
	return b.compose.LoadProject(ctx, projectLoadOptions(ref))
}

func projectLoadOptions(ref updater.ProjectRef) api.ProjectLoadOptions {
	return api.ProjectLoadOptions{
		ProjectName: ref.Name,
		ConfigPaths: append([]string(nil), ref.ConfigPaths...),
		WorkingDir:  ref.WorkingDir,
		EnvFiles:    append([]string(nil), ref.EnvFiles...),
		Services:    append([]string(nil), ref.Services...),
	}
}

func (b *Backend) NewProjectSession(project *types.Project) (updater.ProjectSession, error) {
	service, err := b.composeForProject()
	if err != nil {
		return nil, err
	}
	return &projectSession{compose: service, project: project}, nil
}

func pullOptions() api.PullOptions {
	return api.PullOptions{
		Quiet:           false,
		IgnoreFailures:  false,
		IgnoreBuildable: true,
	}
}

type projectSession struct {
	compose composeAPI
	project *types.Project
}

func (s *projectSession) Pull(ctx context.Context) error {
	return s.compose.Pull(ctx, s.project, pullOptions())
}

func (s *projectSession) Up(ctx context.Context) error {
	return s.compose.Up(ctx, s.project, upOptions(s.project))
}

func (b *Backend) composeForProject() (composeAPI, error) {
	if b.newProjectCompose == nil {
		return b.compose, nil
	}
	service, err := b.newProjectCompose()
	if err != nil {
		return nil, fmt.Errorf("create Compose progress session: %w", err)
	}
	return service, nil
}

func upOptions(project *types.Project) api.UpOptions {
	services := project.ServiceNames()
	return api.UpOptions{
		Create: api.CreateOptions{
			Build: &api.BuildOptions{
				Pull:     false,
				Services: services,
			},
			Services:             services,
			RemoveOrphans:        false,
			IgnoreOrphans:        false,
			Recreate:             api.RecreateDiverged,
			RecreateDependencies: api.RecreateDiverged,
			Inherit:              true,
			QuietPull:            false,
		},
		Start: api.StartOptions{
			Project:  project,
			Services: services,
		},
	}
}

func (b *Backend) PruneImages(ctx context.Context) error {
	_, err := b.engine.ImagePrune(ctx, client.ImagePruneOptions{})
	return err
}
