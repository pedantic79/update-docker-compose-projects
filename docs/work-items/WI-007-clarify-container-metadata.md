# WI-007: Clarify container metadata and project-configuration comparison

- **File**: [internal/docker/discovery.go](../../internal/docker/discovery.go#L44)
- **Severity**: Suggestion
- **Impact**: `projectMetadata` is populated for each container, while its `matches` method compares only project-wide fields. Its stored project name is redundant after validation. More precise names can document the intentional split without moving service-label parsing back into the caller.

## Description

Each non-one-off container is parsed into metadata containing its service and
the Compose launch configuration repeated on that container. The first parsed
value becomes the baseline for project-wide consistency checks. The service is
correctly excluded from those checks because one project can contain multiple
services and replicas.

Rename the per-container value accordingly, name the comparison after the
subset it checks, and remove `projectName` from the struct. Continue validating
the raw project label in `metadataFromContainer`; do not split service-label
validation and extraction across multiple functions.

## Proposed Fix

```diff
--- a/internal/docker/discovery.go
+++ b/internal/docker/discovery.go
@@
 func projectRefFromContainers(projectName string, containers []api.ContainerSummary) (updater.ProjectRef, error) {
 	ref := updater.ProjectRef{Name: projectName}
-	var metadata *projectMetadata
+	var baseline *containerMetadata
@@
 		current, err := metadataFromContainer(projectName, summary)
 		if err != nil {
 			return updater.ProjectRef{}, err
 		}
-		if metadata == nil {
-			metadata = &current
-		} else if err := metadata.matches(projectName, summary.ID, current); err != nil {
+		if baseline == nil {
+			baseline = &current
+		} else if err := baseline.matchesProjectConfig(projectName, summary.ID, current); err != nil {
 			return updater.ProjectRef{}, err
 		}
@@
-	ref.ConfigPaths = splitLabelList(metadata.configFiles)
+	ref.ConfigPaths = splitLabelList(baseline.configFiles)
 	ref.Status = formatStateCounts(states)
-	ref.WorkingDir = metadata.workingDir
-	ref.EnvFiles = splitLabelList(metadata.envFiles)
+	ref.WorkingDir = baseline.workingDir
+	ref.EnvFiles = splitLabelList(baseline.envFiles)
@@
-type projectMetadata struct {
-	projectName string
+type containerMetadata struct {
 	configFiles string
 	workingDir  string
 	envFiles    string
 	service     string
 }
 
-func metadataFromContainer(projectName string, summary api.ContainerSummary) (projectMetadata, error) {
+func metadataFromContainer(projectName string, summary api.ContainerSummary) (containerMetadata, error) {
@@
-			return projectMetadata{}, fmt.Errorf(
+			return containerMetadata{}, fmt.Errorf(
@@
-		return projectMetadata{}, fmt.Errorf(
+		return containerMetadata{}, fmt.Errorf(
@@
-	return projectMetadata{
-		projectName: summary.Labels[api.ProjectLabel],
+	return containerMetadata{
 		configFiles: summary.Labels[api.ConfigFilesLabel],
 		workingDir:  summary.Labels[api.WorkingDirLabel],
 		envFiles:    summary.Labels[api.EnvironmentFileLabel],
 		service:     summary.Labels[api.ServiceLabel],
 	}, nil
 }
 
-func (m projectMetadata) matches(projectName, containerID string, other projectMetadata) error {
+func (m containerMetadata) matchesProjectConfig(
+	projectName string,
+	containerID string,
+	other containerMetadata,
+) error {
 	checks := []struct {
 		label string
 		left  string
 		right string
 	}{
-		{api.ProjectLabel, m.projectName, other.projectName},
 		{api.ConfigFilesLabel, m.configFiles, other.configFiles},
 		{api.WorkingDirLabel, m.workingDir, other.workingDir},
 		{api.EnvironmentFileLabel, m.envFiles, other.envFiles},
```

The project-label validation remains concrete and unchanged:

```go
	if actual := summary.Labels[api.ProjectLabel]; actual != projectName {
		return containerMetadata{}, fmt.Errorf(
			"project %q: container %q: project label is %q",
			projectName,
			summary.ID,
			actual,
		)
	}
```

## Acceptance Plan

1. Preserve extraction of multiple services and replicas in
   `TestProjectRefFromContainersPreservesMetadataAndTypedState`.
2. Preserve rejection of missing labels, incorrect project labels, and
   inconsistent project configuration in
   `TestProjectRefFromContainersRejectsInvalidMetadata`.
3. Confirm `service` remains parsed once by `metadataFromContainer` and remains
   excluded from project-configuration comparison.
4. Run `just fmt`, `just check`, and `just coverage`; total statement coverage
   must remain above 95%.

## Action Items

- [x] Rename `projectMetadata` to `containerMetadata`.
- [x] Rename `matches` to `matchesProjectConfig`.
- [x] Remove redundant stored project-name state and comparison.
- [x] Complete the acceptance plan.
