package docker

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/pedantic79/update-docker-compose-projects/internal/updater"
)

func TestBackendForwardsTypedOptionsWithoutDocker(t *testing.T) {
	t.Parallel()

	project := &types.Project{
		Name: "billing",
		Services: types.Services{
			"worker": {Name: "worker"},
			"web":    {Name: "web"},
		},
	}
	compose := &fakeCompose{loadResult: project}
	engine := &fakeEngine{}
	backend := &Backend{compose: compose, engine: engine, context: "remote"}
	ref := updater.ProjectRef{
		Name:        "billing",
		ConfigPaths: []string{"/srv/base.yml", "/srv/prod.yml"},
		WorkingDir:  "/srv/billing",
		EnvFiles:    []string{"/srv/prod.env"},
		Services:    []string{"web", "worker"},
	}

	loaded, err := backend.LoadProject(context.Background(), ref)
	if err != nil || loaded != project {
		t.Fatalf("LoadProject() = (%p, %v), want (%p, nil)", loaded, err, project)
	}
	wantLoad := api.ProjectLoadOptions{
		ProjectName: "billing",
		ConfigPaths: []string{"/srv/base.yml", "/srv/prod.yml"},
		WorkingDir:  "/srv/billing",
		EnvFiles:    []string{"/srv/prod.env"},
		Services:    []string{"web", "worker"},
	}
	if !reflect.DeepEqual(compose.loadOptions, wantLoad) {
		t.Fatalf("load options = %#v, want %#v", compose.loadOptions, wantLoad)
	}

	session, err := backend.NewProjectSession(project)
	if err != nil {
		t.Fatalf("NewProjectSession() error = %v", err)
	}
	if err := session.Pull(context.Background()); err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	if compose.pullProject != project {
		t.Fatal("Pull() did not forward project")
	}
	if compose.pullOptions != (api.PullOptions{IgnoreBuildable: true}) {
		t.Fatalf("pull options = %#v", compose.pullOptions)
	}

	if err := session.Up(context.Background()); err != nil {
		t.Fatalf("Up() error = %v", err)
	}
	options := compose.upOptions
	services := []string{"web", "worker"}
	if options.Create.Recreate != api.RecreateDiverged || options.Create.RecreateDependencies != api.RecreateDiverged {
		t.Fatalf("recreate policy = %q/%q, want diverged", options.Create.Recreate, options.Create.RecreateDependencies)
	}
	if options.Create.RemoveOrphans {
		t.Fatal("RemoveOrphans = true; selected running services must not remove stopped services")
	}
	if !options.Create.Inherit || options.Create.Build == nil || options.Create.Build.Pull {
		t.Fatalf("create/build options = %#v", options.Create)
	}
	if !reflect.DeepEqual(options.Create.Services, services) ||
		!reflect.DeepEqual(options.Create.Build.Services, services) ||
		!reflect.DeepEqual(options.Start.Services, services) {
		t.Fatalf("service selections = create:%v build:%v start:%v", options.Create.Services, options.Create.Build.Services, options.Start.Services)
	}
	if options.Start.Project != project {
		t.Fatal("start options did not preserve project")
	}

	if err := backend.PruneImages(context.Background()); err != nil {
		t.Fatalf("PruneImages() error = %v", err)
	}
	if engine.pruneCalls != 1 {
		t.Fatalf("prune calls = %d, want 1", engine.pruneCalls)
	}
	if backend.Context() != "remote" {
		t.Fatalf("Context() = %q", backend.Context())
	}
	if err := backend.Close(); err != nil || engine.closeCalls != 1 {
		t.Fatalf("Close() = %v, calls = %d", err, engine.closeCalls)
	}
}

func TestBackendPropagatesComposeAndEngineErrors(t *testing.T) {
	t.Parallel()

	loadErr := errors.New("load")
	pullErr := errors.New("pull")
	upErr := errors.New("up")
	pruneErr := errors.New("prune")
	closeErr := errors.New("close")
	compose := &fakeCompose{loadErr: loadErr, pullErr: pullErr, upErr: upErr}
	engine := &fakeEngine{pruneErr: pruneErr, closeErr: closeErr}
	backend := &Backend{compose: compose, engine: engine}
	project := &types.Project{Name: "app"}

	if _, err := backend.LoadProject(context.Background(), updater.ProjectRef{}); !errors.Is(err, loadErr) {
		t.Errorf("LoadProject() error = %v", err)
	}
	session, err := backend.NewProjectSession(project)
	if err != nil {
		t.Fatalf("NewProjectSession() error = %v", err)
	}
	if err := session.Pull(context.Background()); !errors.Is(err, pullErr) {
		t.Errorf("Pull() error = %v", err)
	}
	if err := session.Up(context.Background()); !errors.Is(err, upErr) {
		t.Errorf("Up() error = %v", err)
	}
	if err := backend.PruneImages(context.Background()); !errors.Is(err, pruneErr) {
		t.Errorf("PruneImages() error = %v", err)
	}
	if err := backend.Close(); !errors.Is(err, closeErr) {
		t.Errorf("Close() error = %v", err)
	}
}

func TestBackendUsesOneFreshProgressSessionPerProject(t *testing.T) {
	t.Parallel()

	firstCompose := &fakeCompose{}
	secondCompose := &fakeCompose{}
	composes := []composeAPI{firstCompose, secondCompose}
	factoryCalls := 0
	backend := &Backend{
		compose: &fakeCompose{},
		engine:  &fakeEngine{},
		newProjectCompose: func() (composeAPI, error) {
			service := composes[factoryCalls]
			factoryCalls++
			return service, nil
		},
	}
	firstProject := &types.Project{
		Name:     "first",
		Services: types.Services{"web": {Name: "web"}},
	}
	secondProject := &types.Project{
		Name:     "second",
		Services: types.Services{"worker": {Name: "worker"}},
	}

	firstSession, err := backend.NewProjectSession(firstProject)
	if err != nil {
		t.Fatalf("first NewProjectSession() error = %v", err)
	}
	if err := firstSession.Pull(context.Background()); err != nil {
		t.Fatalf("first Pull() error = %v", err)
	}
	if err := firstSession.Up(context.Background()); err != nil {
		t.Fatalf("first Up() error = %v", err)
	}

	secondSession, err := backend.NewProjectSession(secondProject)
	if err != nil {
		t.Fatalf("second NewProjectSession() error = %v", err)
	}
	if err := secondSession.Pull(context.Background()); err != nil {
		t.Fatalf("second Pull() error = %v", err)
	}
	if err := secondSession.Up(context.Background()); err != nil {
		t.Fatalf("second Up() error = %v", err)
	}
	if factoryCalls != 2 {
		t.Fatalf("progress session factory calls = %d, want one per project (2)", factoryCalls)
	}
	if firstCompose.pullProject != firstProject || firstCompose.upProject != firstProject {
		t.Fatalf("first Compose session received pull:%p up:%p", firstCompose.pullProject, firstCompose.upProject)
	}
	if secondCompose.pullProject != secondProject || secondCompose.upProject != secondProject {
		t.Fatalf("second Compose session received pull:%p up:%p", secondCompose.pullProject, secondCompose.upProject)
	}
}

func TestBackendReturnsProgressSessionCreationError(t *testing.T) {
	t.Parallel()

	factoryErr := errors.New("renderer failed")
	backend := &Backend{
		compose: &fakeCompose{},
		engine:  &fakeEngine{},
		newProjectCompose: func() (composeAPI, error) {
			return nil, factoryErr
		},
	}
	project := &types.Project{Name: "app"}

	_, err := backend.NewProjectSession(project)
	if !errors.Is(err, factoryErr) {
		t.Fatalf("NewProjectSession() error = %v, want factory error", err)
	}
}

func TestOptionBuildersCopyCallerSlices(t *testing.T) {
	t.Parallel()

	ref := updater.ProjectRef{
		ConfigPaths: []string{"compose.yml"},
		EnvFiles:    []string{"prod.env"},
		Services:    []string{"web"},
	}
	options := projectLoadOptions(ref)
	ref.ConfigPaths[0] = "changed.yml"
	ref.EnvFiles[0] = "changed.env"
	ref.Services[0] = "worker"

	if options.ConfigPaths[0] != "compose.yml" || options.EnvFiles[0] != "prod.env" || options.Services[0] != "web" {
		t.Fatalf("options alias caller slices: %#v", options)
	}
	if pullOptions() != (api.PullOptions{IgnoreBuildable: true}) {
		t.Fatalf("pullOptions() = %#v", pullOptions())
	}
}
