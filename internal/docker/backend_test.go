package docker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/compose/v5/pkg/compose"
	"github.com/pedantic79/update-docker-compose-projects/internal/updater"
)

func TestNewBuildsBackendWithoutConnectingToDaemon(t *testing.T) {
	t.Setenv("DOCKER_CONFIG", t.TempDir())
	t.Setenv("DOCKER_CONTEXT", "default")
	t.Setenv("DOCKER_HOST", "unix:///nonexistent/docker.sock")

	backend, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := backend.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	if backend.compose == nil || backend.engine == nil || backend.newProjectCompose == nil {
		t.Fatalf("New() returned incomplete backend: %#v", backend)
	}
	if backend.Context() != "default" {
		t.Fatalf("Context() = %q, want default", backend.Context())
	}
	if _, err := backend.composeForProject(); err != nil {
		t.Fatalf("composeForProject() error = %v", err)
	}
}

func TestBackendForwardsTypedOptionsWithoutDocker(t *testing.T) {
	t.Parallel()

	project := &types.Project{
		Name: "billing",
		Services: types.Services{
			"db":     {Name: "db"},
			"worker": {Name: "worker"},
			"web": {
				Name:      "web",
				DependsOn: map[string]types.ServiceDependency{"db": {Required: true}},
			},
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

	session, err := backend.OpenProject(context.Background(), ref)
	if err != nil {
		t.Fatalf("OpenProject() error = %v", err)
	}
	loaded := projectFromSession(t, session)
	services := []string{"web", "worker"}
	if !reflect.DeepEqual(loaded.ServiceNames(), services) {
		t.Fatalf("loaded services = %v, want %v", loaded.ServiceNames(), services)
	}
	if dependencies := loaded.Services["web"].DependsOn; len(dependencies) != 0 {
		t.Fatalf("loaded web dependencies = %v, want none", dependencies)
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

	if err := session.Pull(context.Background()); err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	if compose.pullProject != loaded {
		t.Fatal("Pull() did not forward project")
	}
	if compose.pullOptions != (api.PullOptions{IgnoreBuildable: true}) {
		t.Fatalf("pull options = %#v", compose.pullOptions)
	}

	if err := session.Up(context.Background()); err != nil {
		t.Fatalf("Up() error = %v", err)
	}
	options := compose.upOptions
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
	if options.Start.Project != loaded {
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

func TestBackendKeepsDependencyWhenItIsAlsoSelected(t *testing.T) {
	t.Parallel()

	project := &types.Project{
		Name: "billing",
		Services: types.Services{
			"db": {Name: "db"},
			"web": {
				Name:      "web",
				DependsOn: map[string]types.ServiceDependency{"db": {Required: true}},
			},
		},
	}
	backend := &Backend{compose: &fakeCompose{loadResult: project}, engine: &fakeEngine{}}

	session, err := backend.OpenProject(context.Background(), updater.ProjectRef{
		Name:     "billing",
		Services: []string{"web", "db"},
	})
	if err != nil {
		t.Fatalf("OpenProject() error = %v", err)
	}
	loaded := projectFromSession(t, session)
	services := []string{"db", "web"}
	if !reflect.DeepEqual(loaded.ServiceNames(), services) {
		t.Fatalf("loaded services = %v, want %v", loaded.ServiceNames(), services)
	}
	if _, ok := loaded.Services["web"].DependsOn["db"]; !ok {
		t.Fatal("selected web dependency db was removed")
	}
}

func TestBackendLoadsSelectedProfiledServiceWithoutItsStoppedDependency(t *testing.T) {
	t.Parallel()

	workingDir := t.TempDir()
	configPath := filepath.Join(workingDir, "compose.yaml")
	config := []byte(`services:
  db:
    image: postgres
  web:
    image: nginx
    profiles: [debug]
    depends_on:
      - db
`)
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("write Compose config: %v", err)
	}
	service, err := compose.NewComposeService(nil)
	if err != nil {
		t.Fatalf("NewComposeService() error = %v", err)
	}
	backend := &Backend{compose: service, engine: &fakeEngine{}}

	session, err := backend.OpenProject(context.Background(), updater.ProjectRef{
		Name:        "profiled",
		ConfigPaths: []string{configPath},
		WorkingDir:  workingDir,
		Services:    []string{"web"},
	})
	if err != nil {
		t.Fatalf("OpenProject() error = %v", err)
	}
	project := projectFromSession(t, session)
	if services := project.ServiceNames(); !reflect.DeepEqual(services, []string{"web"}) {
		t.Fatalf("loaded services = %v, want [web]", services)
	}
	if !slices.Contains(project.Profiles, "debug") {
		t.Fatalf("active profiles = %v, want debug to be active", project.Profiles)
	}
	if dependencies := project.Services["web"].DependsOn; len(dependencies) != 0 {
		t.Fatalf("loaded web dependencies = %v, want none", dependencies)
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

	if _, err := backend.OpenProject(context.Background(), updater.ProjectRef{}); !errors.Is(err, loadErr) {
		t.Errorf("OpenProject() load error = %v", err)
	} else if !strings.Contains(err.Error(), "load Compose project") {
		t.Errorf("OpenProject() load error = %v, want operation context", err)
	}
	compose.loadErr = nil
	compose.loadResult = project
	session, err := backend.OpenProject(context.Background(), updater.ProjectRef{Name: "app"})
	if err != nil {
		t.Fatalf("OpenProject() error = %v", err)
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
	firstProject := &types.Project{
		Name:     "first",
		Services: types.Services{"web": {Name: "web"}},
	}
	secondProject := &types.Project{
		Name:     "second",
		Services: types.Services{"worker": {Name: "worker"}},
	}
	loader := &fakeCompose{loadResult: firstProject}
	backend := &Backend{
		compose: loader,
		engine:  &fakeEngine{},
		newProjectCompose: func() (composeAPI, error) {
			service := composes[factoryCalls]
			factoryCalls++
			return service, nil
		},
	}

	firstSession, err := backend.OpenProject(context.Background(), updater.ProjectRef{
		Name:     "first",
		Services: []string{"web"},
	})
	if err != nil {
		t.Fatalf("first OpenProject() error = %v", err)
	}
	firstOpenedProject := projectFromSession(t, firstSession)
	if err := firstSession.Pull(context.Background()); err != nil {
		t.Fatalf("first Pull() error = %v", err)
	}
	if err := firstSession.Up(context.Background()); err != nil {
		t.Fatalf("first Up() error = %v", err)
	}

	loader.loadResult = secondProject
	secondSession, err := backend.OpenProject(context.Background(), updater.ProjectRef{
		Name:     "second",
		Services: []string{"worker"},
	})
	if err != nil {
		t.Fatalf("second OpenProject() error = %v", err)
	}
	secondOpenedProject := projectFromSession(t, secondSession)
	if err := secondSession.Pull(context.Background()); err != nil {
		t.Fatalf("second Pull() error = %v", err)
	}
	if err := secondSession.Up(context.Background()); err != nil {
		t.Fatalf("second Up() error = %v", err)
	}
	if factoryCalls != 2 {
		t.Fatalf("progress session factory calls = %d, want one per project (2)", factoryCalls)
	}
	if firstCompose.pullProject != firstOpenedProject || firstCompose.upProject != firstOpenedProject {
		t.Fatalf("first Compose session received pull:%p up:%p", firstCompose.pullProject, firstCompose.upProject)
	}
	if secondCompose.pullProject != secondOpenedProject || secondCompose.upProject != secondOpenedProject {
		t.Fatalf("second Compose session received pull:%p up:%p", secondCompose.pullProject, secondCompose.upProject)
	}
}

func TestBackendReturnsProgressSessionCreationError(t *testing.T) {
	t.Parallel()

	factoryErr := errors.New("renderer failed")
	backend := &Backend{
		compose: &fakeCompose{loadResult: &types.Project{
			Name:     "app",
			Services: types.Services{"web": {Name: "web"}},
		}},
		engine: &fakeEngine{},
		newProjectCompose: func() (composeAPI, error) {
			return nil, factoryErr
		},
	}
	_, err := backend.OpenProject(context.Background(), updater.ProjectRef{
		Name:     "app",
		Services: []string{"web"},
	})
	if !errors.Is(err, factoryErr) {
		t.Fatalf("OpenProject() error = %v, want factory error", err)
	}
	if !strings.Contains(err.Error(), "create Compose progress session") {
		t.Fatalf("OpenProject() error = %v, want operation context", err)
	}
}

func TestBackendRejectsUnknownSelectedService(t *testing.T) {
	t.Parallel()

	backend := &Backend{
		compose: &fakeCompose{loadResult: &types.Project{
			Name:     "app",
			Services: types.Services{"web": {Name: "web"}},
		}},
		engine: &fakeEngine{},
	}

	_, err := backend.OpenProject(context.Background(), updater.ProjectRef{
		Name:     "app",
		Services: []string{"missing"},
	})
	if err == nil || !strings.Contains(err.Error(), "select running services") {
		t.Fatalf("OpenProject() error = %v, want service-selection context", err)
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

func projectFromSession(t *testing.T, session updater.ProjectSession) *types.Project {
	t.Helper()

	projectSession, ok := session.(*projectSession)
	if !ok {
		t.Fatalf("session type = %T, want *projectSession", session)
	}
	return projectSession.project
}
