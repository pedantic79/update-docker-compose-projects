package docker

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/types/container"
	"github.com/pedantic79/update-docker-compose-projects/internal/updater"
)

func (b *Backend) DiscoverProjects(ctx context.Context) ([]updater.ProjectRef, error) {
	stacks, err := b.compose.List(ctx, api.ListOptions{All: true})
	if err != nil {
		return nil, err
	}

	refs := make([]updater.ProjectRef, 0, len(stacks))
	for _, stack := range stacks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		containers, err := b.compose.Ps(ctx, stack.Name, api.PsOptions{All: true})
		if err != nil {
			return nil, fmt.Errorf("project %q: list containers: %w", stack.Name, err)
		}

		ref, err := projectRefFromContainers(stack.Name, containers)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}

	slices.SortFunc(refs, func(a, b updater.ProjectRef) int {
		return strings.Compare(a.Name, b.Name)
	})
	return refs, nil
}

func projectRefFromContainers(projectName string, containers []api.ContainerSummary) (updater.ProjectRef, error) {
	ref := updater.ProjectRef{Name: projectName}
	var metadata *projectMetadata
	running := map[string]struct{}{}
	stopped := map[string]struct{}{}
	states := map[string]int{}
	nonOneOff := 0

	for _, summary := range containers {
		if strings.EqualFold(summary.Labels[api.OneoffLabel], "true") {
			continue
		}
		nonOneOff++
		states[string(summary.State)]++

		current, err := metadataFromContainer(projectName, summary)
		if err != nil {
			return updater.ProjectRef{}, err
		}
		if metadata == nil {
			metadata = &current
		} else if err := metadata.matches(projectName, summary.ID, current); err != nil {
			return updater.ProjectRef{}, err
		}

		if summary.State == container.StateRunning {
			running[current.service] = struct{}{}
			delete(stopped, current.service)
		} else if _, isRunning := running[current.service]; !isRunning {
			stopped[current.service] = struct{}{}
		}
	}

	// A project containing only one-off containers is not eligible, but it is
	// still a valid discovery result and can be reported as skipped.
	if nonOneOff == 0 {
		return ref, nil
	}

	ref.ConfigPaths = splitLabelList(metadata.configFiles)
	ref.Status = formatStateCounts(states)
	ref.WorkingDir = metadata.workingDir
	ref.EnvFiles = splitLabelList(metadata.envFiles)
	ref.Services = sortedKeys(running)
	ref.StoppedServices = sortedKeys(stopped)
	return ref, nil
}

func formatStateCounts(states map[string]int) string {
	names := make([]string, 0, len(states))
	for state := range states {
		names = append(names, state)
	}
	slices.Sort(names)

	parts := make([]string, 0, len(names))
	for _, state := range names {
		parts = append(parts, fmt.Sprintf("%s(%d)", state, states[state]))
	}
	return strings.Join(parts, ", ")
}

type projectMetadata struct {
	projectName string
	configFiles string
	workingDir  string
	envFiles    string
	service     string
}

func metadataFromContainer(projectName string, summary api.ContainerSummary) (projectMetadata, error) {
	required := []string{
		api.ProjectLabel,
		api.ConfigFilesLabel,
		api.WorkingDirLabel,
		api.ServiceLabel,
	}
	for _, label := range required {
		if summary.Labels[label] == "" {
			return projectMetadata{}, fmt.Errorf(
				"project %q: container %q: missing label %q",
				projectName,
				summary.ID,
				label,
			)
		}
	}

	if actual := summary.Labels[api.ProjectLabel]; actual != projectName {
		return projectMetadata{}, fmt.Errorf(
			"project %q: container %q: project label is %q",
			projectName,
			summary.ID,
			actual,
		)
	}

	return projectMetadata{
		projectName: summary.Labels[api.ProjectLabel],
		configFiles: summary.Labels[api.ConfigFilesLabel],
		workingDir:  summary.Labels[api.WorkingDirLabel],
		envFiles:    summary.Labels[api.EnvironmentFileLabel],
		service:     summary.Labels[api.ServiceLabel],
	}, nil
}

func (m projectMetadata) matches(projectName, containerID string, other projectMetadata) error {
	checks := []struct {
		label string
		left  string
		right string
	}{
		{api.ProjectLabel, m.projectName, other.projectName},
		{api.ConfigFilesLabel, m.configFiles, other.configFiles},
		{api.WorkingDirLabel, m.workingDir, other.workingDir},
		{api.EnvironmentFileLabel, m.envFiles, other.envFiles},
	}
	for _, check := range checks {
		if check.left != check.right {
			return fmt.Errorf(
				"project %q: container %q: inconsistent label %q",
				projectName,
				containerID,
				check.label,
			)
		}
	}
	return nil
}

func splitLabelList(value string) []string {
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func sortedKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}
