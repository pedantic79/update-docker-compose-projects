package docker

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/pedantic79/update-docker-compose-projects/internal/updater"
)

func TestProjectRefFromContainersPreservesMetadataAndTypedState(t *testing.T) {
	t.Parallel()

	web := composeContainer("web-1", "billing", "web", container.StateExited)
	web.Labels[api.ConfigFilesLabel] = "/srv/base.yml,/srv/prod.yml"
	web.Labels[api.EnvironmentFileLabel] = "/srv/common.env,/srv/prod.env"
	webRunningReplica := composeContainer("web-2", "billing", "web", container.StateRunning)
	webRunningReplica.Labels[api.ConfigFilesLabel] = web.Labels[api.ConfigFilesLabel]
	webRunningReplica.Labels[api.EnvironmentFileLabel] = web.Labels[api.EnvironmentFileLabel]
	worker := composeContainer("worker-1", "billing", "worker", container.StatePaused)
	worker.Labels[api.ConfigFilesLabel] = web.Labels[api.ConfigFilesLabel]
	worker.Labels[api.EnvironmentFileLabel] = web.Labels[api.EnvironmentFileLabel]
	oneOff := api.ContainerSummary{
		ID:     "run-1",
		Labels: map[string]string{api.OneoffLabel: "True"},
		State:  container.StateRunning,
	}

	got, err := projectRefFromContainers("billing", []api.ContainerSummary{
		worker, oneOff, web, webRunningReplica,
	})
	if err != nil {
		t.Fatalf("projectRefFromContainers() error = %v", err)
	}
	want := updater.ProjectRef{
		Name:            "billing",
		Status:          "exited(1), paused(1), running(1)",
		ConfigPaths:     []string{"/srv/base.yml", "/srv/prod.yml"},
		WorkingDir:      "/srv/billing",
		EnvFiles:        []string{"/srv/common.env", "/srv/prod.env"},
		Services:        []string{"web"},
		StoppedServices: []string{"worker"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("project ref = %#v, want %#v", got, want)
	}
}

func TestProjectRefFromContainersOnlyOneOffIsSkipped(t *testing.T) {
	t.Parallel()

	got, err := projectRefFromContainers("tools", []api.ContainerSummary{{
		ID:     "run-1",
		Labels: map[string]string{api.OneoffLabel: "true"},
		State:  container.StateRunning,
	}})
	if err != nil {
		t.Fatalf("projectRefFromContainers() error = %v", err)
	}
	if !reflect.DeepEqual(got, updater.ProjectRef{Name: "tools"}) {
		t.Fatalf("project ref = %#v", got)
	}
}

func TestProjectRefFromContainersRejectsInvalidMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		containers func() []api.ContainerSummary
		wantError  string
	}{
		{
			name: "missing required label",
			containers: func() []api.ContainerSummary {
				value := composeContainer("web-1", "app", "web", container.StateRunning)
				delete(value.Labels, api.WorkingDirLabel)
				return []api.ContainerSummary{value}
			},
			wantError: "missing label \"" + api.WorkingDirLabel + "\"",
		},
		{
			name: "wrong project",
			containers: func() []api.ContainerSummary {
				return []api.ContainerSummary{composeContainer("web-1", "other", "web", container.StateRunning)}
			},
			wantError: "project label is \"other\"",
		},
		{
			name: "inconsistent config files",
			containers: func() []api.ContainerSummary {
				first := composeContainer("web-1", "app", "web", container.StateRunning)
				second := composeContainer("worker-1", "app", "worker", container.StateRunning)
				second.Labels[api.ConfigFilesLabel] = "/different.yml"
				return []api.ContainerSummary{first, second}
			},
			wantError: "inconsistent label \"" + api.ConfigFilesLabel + "\"",
		},
		{
			name: "inconsistent environment files",
			containers: func() []api.ContainerSummary {
				first := composeContainer("web-1", "app", "web", container.StateRunning)
				second := composeContainer("worker-1", "app", "worker", container.StateRunning)
				second.Labels[api.EnvironmentFileLabel] = "/different.env"
				return []api.ContainerSummary{first, second}
			},
			wantError: "inconsistent label \"" + api.EnvironmentFileLabel + "\"",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := projectRefFromContainers("app", test.containers())
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want substring %q", err, test.wantError)
			}
			if !strings.Contains(err.Error(), "project \"app\"") {
				t.Fatalf("error %q does not identify project", err)
			}
		})
	}
}

func TestDiscoverProjectsUsesTypedContainersAndSortsResults(t *testing.T) {
	t.Parallel()

	compose := &fakeCompose{
		stacks: []api.Stack{{Name: "zeta"}, {Name: "alpha"}},
		containers: map[string][]api.ContainerSummary{
			"zeta":  {composeContainer("zeta-web", "zeta", "web", container.StateExited)},
			"alpha": {composeContainer("alpha-web", "alpha", "web", container.StateRunning)},
		},
	}
	backend := &Backend{compose: compose, engine: &fakeEngine{}}

	got, err := backend.DiscoverProjects(context.Background())
	if err != nil {
		t.Fatalf("DiscoverProjects() error = %v", err)
	}
	if !compose.listOptions.All {
		t.Fatal("ListOptions.All = false, want true")
	}
	for name, options := range compose.psOptions {
		if !options.All {
			t.Errorf("PsOptions.All for %s = false, want true", name)
		}
	}
	if gotNames := []string{got[0].Name, got[1].Name}; !reflect.DeepEqual(gotNames, []string{"alpha", "zeta"}) {
		t.Fatalf("project order = %v", gotNames)
	}
	if !got[0].Eligible() || got[1].Eligible() {
		t.Fatalf("eligibility = alpha:%v zeta:%v", got[0].Eligible(), got[1].Eligible())
	}
}

func TestDiscoverProjectsReturnsListAndProjectErrors(t *testing.T) {
	t.Parallel()

	listErr := errors.New("list failed")
	backend := &Backend{compose: &fakeCompose{listErr: listErr}, engine: &fakeEngine{}}
	if _, err := backend.DiscoverProjects(context.Background()); !errors.Is(err, listErr) {
		t.Fatalf("list error = %v", err)
	}

	psErr := errors.New("ps failed")
	backend = &Backend{compose: &fakeCompose{
		stacks:   []api.Stack{{Name: "app"}},
		psErrors: map[string]error{"app": psErr},
	}, engine: &fakeEngine{}}
	_, err := backend.DiscoverProjects(context.Background())
	if !errors.Is(err, psErr) || !strings.Contains(err.Error(), "project \"app\"") {
		t.Fatalf("project list error = %v", err)
	}

	invalid := composeContainer("web-1", "app", "web", container.StateRunning)
	delete(invalid.Labels, api.ConfigFilesLabel)
	backend = &Backend{compose: &fakeCompose{
		stacks:     []api.Stack{{Name: "app"}},
		containers: map[string][]api.ContainerSummary{"app": {invalid}},
	}, engine: &fakeEngine{}}
	_, err = backend.DiscoverProjects(context.Background())
	if err == nil || !strings.Contains(err.Error(), "missing label") {
		t.Fatalf("metadata error = %v", err)
	}
}

func TestDiscoverProjectsHonorsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	compose := &fakeCompose{stacks: []api.Stack{{Name: "app"}}}
	backend := &Backend{compose: compose, engine: &fakeEngine{}}

	_, err := backend.DiscoverProjects(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("DiscoverProjects() error = %v, want canceled", err)
	}
	if len(compose.psOptions) != 0 {
		t.Fatalf("Ps calls = %v, want none", compose.psOptions)
	}
}

func composeContainer(id, project, service string, state container.ContainerState) api.ContainerSummary {
	return api.ContainerSummary{
		ID:    id,
		State: state,
		Labels: map[string]string{
			api.ProjectLabel:     project,
			api.ConfigFilesLabel: "/srv/" + project + "/compose.yml",
			api.WorkingDirLabel:  "/srv/" + project,
			api.ServiceLabel:     service,
			api.OneoffLabel:      "False",
		},
	}
}

type fakeCompose struct {
	stacks      []api.Stack
	listErr     error
	listOptions api.ListOptions
	containers  map[string][]api.ContainerSummary
	psErrors    map[string]error
	psOptions   map[string]api.PsOptions
	loadResult  *types.Project
	loadErr     error
	loadOptions api.ProjectLoadOptions
	pullErr     error
	pullProject *types.Project
	pullOptions api.PullOptions
	upErr       error
	upProject   *types.Project
	upOptions   api.UpOptions
}

func (f *fakeCompose) List(_ context.Context, options api.ListOptions) ([]api.Stack, error) {
	f.listOptions = options
	return f.stacks, f.listErr
}

func (f *fakeCompose) Ps(_ context.Context, project string, options api.PsOptions) ([]api.ContainerSummary, error) {
	if f.psOptions == nil {
		f.psOptions = map[string]api.PsOptions{}
	}
	f.psOptions[project] = options
	return f.containers[project], f.psErrors[project]
}

func (f *fakeCompose) LoadProject(_ context.Context, options api.ProjectLoadOptions) (*types.Project, error) {
	f.loadOptions = options
	return f.loadResult, f.loadErr
}

func (f *fakeCompose) Pull(_ context.Context, project *types.Project, options api.PullOptions) error {
	f.pullProject = project
	f.pullOptions = options
	return f.pullErr
}

func (f *fakeCompose) Up(_ context.Context, project *types.Project, options api.UpOptions) error {
	f.upProject = project
	f.upOptions = options
	return f.upErr
}

type fakeEngine struct {
	pruneErr     error
	pruneOptions client.ImagePruneOptions
	pruneCalls   int
	closeErr     error
	closeCalls   int
}

func (f *fakeEngine) ImagePrune(_ context.Context, options client.ImagePruneOptions) (client.ImagePruneResult, error) {
	f.pruneCalls++
	f.pruneOptions = options
	return client.ImagePruneResult{}, f.pruneErr
}

func (f *fakeEngine) Close() error {
	f.closeCalls++
	return f.closeErr
}
