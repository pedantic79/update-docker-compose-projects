package updater

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
)

func TestProjectRefEligible(t *testing.T) {
	t.Parallel()

	if (ProjectRef{}).Eligible() {
		t.Fatal("project without running services should not be eligible")
	}
	if !(ProjectRef{Services: []string{"web"}}).Eligible() {
		t.Fatal("project with a running service should be eligible")
	}
}

func TestRunConvergesEligibleProjectsAndSkipsStoppedProjects(t *testing.T) {
	t.Parallel()

	backend := &fakeBackend{projects: []ProjectRef{
		{Name: "stopped", StoppedServices: []string{"worker"}},
		{Name: "running", Services: []string{"web"}},
	}}
	reporter := &recordingReporter{}

	result, err := New(backend, reporter).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	wantCalls := []string{"discover", "load:running", "pull:running", "up:running", "prune"}
	if !reflect.DeepEqual(backend.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", backend.calls, wantCalls)
	}
	if len(result.Projects) != 2 {
		t.Fatalf("project results = %d, want 2", len(result.Projects))
	}
	if result.Projects[0].Status != ProjectSkipped || result.Projects[0].Reason != "no running services" {
		t.Fatalf("stopped result = %+v", result.Projects[0])
	}
	if result.Projects[1].Status != ProjectConverged {
		t.Fatalf("running status = %q, want %q", result.Projects[1].Status, ProjectConverged)
	}
	if !result.PruneAttempted || !result.Pruned {
		t.Fatalf("prune result = attempted:%v pruned:%v", result.PruneAttempted, result.Pruned)
	}
	wantEvents := []string{
		"start:stopped", "finish:stopped:skipped",
		"start:running", "finish:running:converged",
		"prune:start", "prune:finish:ok",
	}
	if !reflect.DeepEqual(reporter.events, wantEvents) {
		t.Fatalf("reporter events = %v, want %v", reporter.events, wantEvents)
	}
}

func TestRunDiscoveryFailureIsFatalBeforeMutation(t *testing.T) {
	t.Parallel()

	discoverErr := errors.New("daemon unavailable")
	backend := &fakeBackend{discoverErr: discoverErr}

	result, err := New(backend).Run(context.Background())
	if !errors.Is(err, discoverErr) {
		t.Fatalf("Run() error = %v, want discover error", err)
	}
	if !reflect.DeepEqual(backend.calls, []string{"discover"}) {
		t.Fatalf("calls = %v, want discovery only", backend.calls)
	}
	if len(result.Projects) != 0 || result.PruneAttempted {
		t.Fatalf("result after discovery error = %+v", result)
	}
}

func TestRunIsolatesAndAggregatesProjectFailures(t *testing.T) {
	t.Parallel()

	loadErr := errors.New("bad compose file")
	sessionErr := errors.New("progress unavailable")
	pullErr := errors.New("registry unavailable")
	upErr := errors.New("create failed")
	backend := &fakeBackend{
		projects: []ProjectRef{
			{Name: "load-fails", Services: []string{"web"}},
			{Name: "session-fails", Services: []string{"web"}},
			{Name: "pull-fails", Services: []string{"web"}},
			{Name: "up-fails", Services: []string{"web"}},
			{Name: "succeeds", Services: []string{"web"}},
		},
		loadErrors:    map[string]error{"load-fails": loadErr},
		sessionErrors: map[string]error{"session-fails": sessionErr},
		pullErrors:    map[string]error{"pull-fails": pullErr},
		upErrors:      map[string][]error{"up-fails": {upErr}},
	}

	result, err := New(backend).Run(context.Background())
	for _, target := range []error{loadErr, sessionErr, pullErr, upErr} {
		if !errors.Is(err, target) {
			t.Errorf("Run() error = %v, want errors.Is(_, %v)", err, target)
		}
	}
	for _, name := range []string{"load-fails", "session-fails", "pull-fails", "up-fails"} {
		if !contains(err.Error(), "project \""+name+"\"") {
			t.Errorf("Run() error %q does not identify project %q", err, name)
		}
	}

	wantCalls := []string{
		"discover",
		"load:load-fails",
		"load:session-fails",
		"load:pull-fails", "pull:pull-fails",
		"load:up-fails", "pull:up-fails", "up:up-fails",
		"load:succeeds", "pull:succeeds", "up:succeeds",
		"prune",
	}
	if !reflect.DeepEqual(backend.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", backend.calls, wantCalls)
	}
	if got := statuses(result.Projects); !reflect.DeepEqual(got, []ProjectStatus{
		ProjectFailed, ProjectFailed, ProjectFailed, ProjectFailed, ProjectConverged,
	}) {
		t.Fatalf("statuses = %v", got)
	}
	if !result.Pruned {
		t.Fatal("successful pulls should cause one final prune")
	}
}

func TestRunDoesNotPruneWhenEveryPullFails(t *testing.T) {
	t.Parallel()

	pullErr := errors.New("pull failed")
	backend := &fakeBackend{
		projects:   []ProjectRef{{Name: "app", Services: []string{"web"}}},
		pullErrors: map[string]error{"app": pullErr},
	}

	result, err := New(backend).Run(context.Background())
	if !errors.Is(err, pullErr) {
		t.Fatalf("Run() error = %v, want pull error", err)
	}
	if result.PruneAttempted {
		t.Fatal("prune should not be attempted without a successful pull")
	}
	if got := backend.calls[len(backend.calls)-1]; got == "prune" {
		t.Fatal("unexpected prune call")
	}
}

func TestRunReturnsPruneFailure(t *testing.T) {
	t.Parallel()

	pruneErr := errors.New("prune failed")
	backend := &fakeBackend{
		projects: []ProjectRef{{Name: "app", Services: []string{"web"}}},
		pruneErr: pruneErr,
	}
	reporter := &recordingReporter{}

	result, err := New(backend, reporter).Run(context.Background())
	if !errors.Is(err, pruneErr) {
		t.Fatalf("Run() error = %v, want prune error", err)
	}
	if !result.PruneAttempted || result.Pruned {
		t.Fatalf("prune result = attempted:%v pruned:%v", result.PruneAttempted, result.Pruned)
	}
	if got := reporter.events[len(reporter.events)-1]; got != "prune:finish:error" {
		t.Fatalf("last reporter event = %q", got)
	}
}

func TestRunRetriesConvergenceAfterUpFailure(t *testing.T) {
	t.Parallel()

	firstUpErr := errors.New("temporary up failure")
	backend := &fakeBackend{
		projects: []ProjectRef{{Name: "app", Services: []string{"web"}}},
		upErrors: map[string][]error{"app": {firstUpErr, nil}},
	}
	service := New(backend)

	if _, err := service.Run(context.Background()); !errors.Is(err, firstUpErr) {
		t.Fatalf("first Run() error = %v, want up error", err)
	}
	if _, err := service.Run(context.Background()); err != nil {
		t.Fatalf("second Run() error = %v", err)
	}

	if countCalls(backend.calls, "pull:app") != 2 || countCalls(backend.calls, "up:app") != 2 {
		t.Fatalf("calls = %v, want pull and up on both runs", backend.calls)
	}
}

func TestRunHonorsCancellationBeforeDiscovery(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	backend := &fakeBackend{}

	result, err := New(backend).Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context cancellation", err)
	}
	if len(backend.calls) != 0 {
		t.Fatalf("calls = %v, want none", backend.calls)
	}
	if result.PruneAttempted {
		t.Fatal("canceled run should not prune")
	}
}

func TestRunStopsSchedulingAndSkipsPruneAfterCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	backend := &fakeBackend{
		projects: []ProjectRef{
			{Name: "first", Services: []string{"web"}},
			{Name: "second", Services: []string{"web"}},
		},
	}
	backend.afterUp = func(name string) {
		if name == "first" {
			cancel()
		}
	}

	result, err := New(backend).Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context cancellation", err)
	}
	wantCalls := []string{"discover", "load:first", "pull:first", "up:first"}
	if !reflect.DeepEqual(backend.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", backend.calls, wantCalls)
	}
	if result.PruneAttempted {
		t.Fatal("canceled run should not begin pruning")
	}
}

func TestRunStopsWhenBackendOperationObservesCancellation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		configure func(*fakeBackend, context.CancelFunc)
		wantCalls []string
	}{
		{
			name: "during pull",
			configure: func(backend *fakeBackend, cancel context.CancelFunc) {
				backend.pullErrors = map[string]error{"first": context.Canceled}
				backend.afterPull = func(string) { cancel() }
			},
			wantCalls: []string{"discover", "load:first", "pull:first"},
		},
		{
			name: "during up",
			configure: func(backend *fakeBackend, cancel context.CancelFunc) {
				backend.upErrors = map[string][]error{"first": {context.Canceled}}
				backend.afterUp = func(string) { cancel() }
			},
			wantCalls: []string{"discover", "load:first", "pull:first", "up:first"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			backend := &fakeBackend{projects: []ProjectRef{
				{Name: "first", Services: []string{"web"}},
				{Name: "second", Services: []string{"web"}},
			}}
			test.configure(backend, cancel)

			result, err := New(backend).Run(ctx)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Run() error = %v, want cancellation", err)
			}
			if !reflect.DeepEqual(backend.calls, test.wantCalls) {
				t.Fatalf("calls = %v, want %v", backend.calls, test.wantCalls)
			}
			if result.PruneAttempted {
				t.Fatal("canceled run should not prune")
			}
		})
	}
}

type fakeBackend struct {
	projects      []ProjectRef
	discoverErr   error
	loadErrors    map[string]error
	sessionErrors map[string]error
	pullErrors    map[string]error
	upErrors      map[string][]error
	pruneErr      error
	afterPull     func(string)
	afterUp       func(string)
	calls         []string
}

type recordingReporter struct {
	events []string
}

func (r *recordingReporter) ProjectStarted(project ProjectRef) {
	r.events = append(r.events, "start:"+project.Name)
}

func (r *recordingReporter) ProjectFinished(project ProjectResult) {
	r.events = append(r.events, "finish:"+project.Name+":"+string(project.Status))
}

func (r *recordingReporter) PruneStarted() {
	r.events = append(r.events, "prune:start")
}

func (r *recordingReporter) PruneFinished(err error) {
	status := "ok"
	if err != nil {
		status = "error"
	}
	r.events = append(r.events, "prune:finish:"+status)
}

func (f *fakeBackend) DiscoverProjects(context.Context) ([]ProjectRef, error) {
	f.calls = append(f.calls, "discover")
	return f.projects, f.discoverErr
}

func (f *fakeBackend) LoadProject(_ context.Context, ref ProjectRef) (*types.Project, error) {
	f.calls = append(f.calls, "load:"+ref.Name)
	if err := f.loadErrors[ref.Name]; err != nil {
		return nil, err
	}
	return &types.Project{Name: ref.Name}, nil
}

func (f *fakeBackend) NewProjectSession(project *types.Project) (ProjectSession, error) {
	if err := f.sessionErrors[project.Name]; err != nil {
		return nil, err
	}
	return fakeProjectSession{backend: f, project: project}, nil
}

type fakeProjectSession struct {
	backend *fakeBackend
	project *types.Project
}

func (s fakeProjectSession) Pull(context.Context) error {
	s.backend.calls = append(s.backend.calls, "pull:"+s.project.Name)
	if s.backend.afterPull != nil {
		s.backend.afterPull(s.project.Name)
	}
	return s.backend.pullErrors[s.project.Name]
}

func (s fakeProjectSession) Up(context.Context) error {
	s.backend.calls = append(s.backend.calls, "up:"+s.project.Name)
	var err error
	if sequence := s.backend.upErrors[s.project.Name]; len(sequence) > 0 {
		err = sequence[0]
		s.backend.upErrors[s.project.Name] = sequence[1:]
	}
	if s.backend.afterUp != nil {
		s.backend.afterUp(s.project.Name)
	}
	return err
}

func (f *fakeBackend) PruneImages(context.Context) error {
	f.calls = append(f.calls, "prune")
	return f.pruneErr
}

func statuses(results []ProjectResult) []ProjectStatus {
	result := make([]ProjectStatus, len(results))
	for i := range results {
		result[i] = results[i].Status
	}
	return result
}

func countCalls(calls []string, target string) int {
	count := 0
	for _, call := range calls {
		if call == target {
			count++
		}
	}
	return count
}

func contains(value, substring string) bool {
	for i := 0; i+len(substring) <= len(value); i++ {
		if value[i:i+len(substring)] == substring {
			return true
		}
	}
	return false
}
