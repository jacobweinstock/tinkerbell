package transform

import (
	"fmt"

	v1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	v2 "github.com/tinkerbell/tinkerbell/api/v1alpha2/tinkerbell"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// templateYAML mirrors the schema of the YAML document stored in
// v1alpha1 Template.Spec.Data. It exists only inside the migrate
// package because Template.Spec.Data has no Go type in either
// v1alpha1 or v1alpha2 — it is an opaque string in the API. When the
// migrate command is retired alongside v1alpha1 support, this file
// goes with it.
type templateYAML struct {
	Version       string         `json:"version,omitempty"`
	Name          string         `json:"name,omitempty"`
	ID            string         `json:"id,omitempty"`
	GlobalTimeout int            `json:"global_timeout,omitempty"`
	Tasks         []templateTask `json:"tasks,omitempty"`
}

type templateTask struct {
	Name        string            `json:"name"`
	WorkerAddr  string            `json:"worker,omitempty"`
	Actions     []templateAction  `json:"actions,omitempty"`
	Volumes     []string          `json:"volumes,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
}

type templateAction struct {
	Name        string            `json:"name"`
	Image       string            `json:"image"`
	Timeout     int64             `json:"timeout,omitempty"`
	Command     []string          `json:"command,omitempty"`
	OnTimeout   []string          `json:"on-timeout,omitempty"`
	OnFailure   []string          `json:"on-failure,omitempty"`
	Volumes     []string          `json:"volumes,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Pid         string            `json:"pid,omitempty"`
}

// Template parses a v1alpha1 Template's embedded YAML and returns one
// v1alpha2 Task per task block in the template. The returned Tasks are
// named:
//
//   - <template-name>                      if the template has a single task
//   - <template-name>-<yaml-task-name>     if the template has multiple tasks
//
// The naming scheme ensures that a Workflow or Policy that referenced a
// Template by name continues to resolve correctly when the template
// produced a single task (the most common case in practice).
//
// Action transforms:
//
//   - command: []string -> Action.Command (first element) plus
//     Action.Args (remaining elements). The v2 Command field requires a
//     unix path. When the first element is not a unix path the entire
//     command slice is preserved in Args and Command is left empty.
//   - timeout: int64 (seconds) -> TimeoutSeconds *int64
//   - environment: map[string]string -> EnvVars []EnvVar (sorted by key
//     for determinism)
//   - pid: string -> Namespaces.PID
//
// Fields dropped (no v2 equivalent): on-timeout, on-failure.
func Template(src *v1.Template) ([]*v2.Task, error) {
	if src == nil {
		return nil, fmt.Errorf("Template: nil source")
	}
	if src.Spec.Data == nil || *src.Spec.Data == "" {
		return nil, fmt.Errorf("Template %s/%s: empty spec.data", src.Namespace, src.Name)
	}

	var parsed templateYAML
	if err := yaml.Unmarshal([]byte(*src.Spec.Data), &parsed); err != nil {
		return nil, fmt.Errorf("Template %s/%s: parse spec.data: %w", src.Namespace, src.Name, err)
	}
	if len(parsed.Tasks) == 0 {
		return nil, fmt.Errorf("Template %s/%s: spec.data has no tasks", src.Namespace, src.Name)
	}

	multi := len(parsed.Tasks) > 1
	used := map[string]int{}
	out := make([]*v2.Task, 0, len(parsed.Tasks))
	for i, t := range parsed.Tasks {
		name := src.Name
		if multi {
			suffix := t.Name
			if suffix == "" {
				suffix = fmt.Sprintf("task-%d", i)
			}
			name = src.Name + "-" + suffix
		}
		if n, ok := used[name]; ok {
			used[name] = n + 1
			name = fmt.Sprintf("%s-%d", name, n+1)
		} else {
			used[name] = 1
		}

		task := &v2.Task{
			TypeMeta: metav1.TypeMeta{
				APIVersion: v2.GroupVersion.String(),
				Kind:       "Task",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Namespace:   src.Namespace,
				Labels:      copyMap(src.Labels),
				Annotations: copyMap(src.Annotations),
			},
			Spec: v2.TaskSpec{
				Actions: convertTemplateActions(t.Actions),
				EnvVars: mapToEnvVars(t.Environment),
				Volumes: stringsToVolumes(t.Volumes),
			},
		}
		out = append(out, task)
	}
	return out, nil
}

func convertTemplateActions(in []templateAction) []v2.Action {
	out := make([]v2.Action, 0, len(in))
	for _, a := range in {
		cmd, args := splitCommand(a.Command)
		act := v2.Action{
			Name:    a.Name,
			Image:   a.Image,
			Command: cmd,
			Args:    args,
			EnvVars: mapToEnvVars(a.Environment),
			Volumes: stringsToVolumes(a.Volumes),
		}
		if a.Timeout > 0 {
			t := a.Timeout
			act.TimeoutSeconds = &t
		}
		if a.Pid != "" {
			act.Namespaces.PID = a.Pid
		}
		out = append(out, act)
	}
	return out
}

// splitCommand converts a v1 command slice into v2 (Command, Args).
// If the first element looks like a unix absolute path it becomes
// Command and the rest become Args. Otherwise everything is stuffed
// into Args and Command is left empty (the image's entrypoint runs).
func splitCommand(in []string) (string, []string) {
	if len(in) == 0 {
		return "", nil
	}
	first := in[0]
	if len(first) > 0 && first[0] == '/' {
		return first, append([]string(nil), in[1:]...)
	}
	return "", append([]string(nil), in...)
}

func stringsToVolumes(in []string) []v2.Volume {
	if len(in) == 0 {
		return nil
	}
	out := make([]v2.Volume, 0, len(in))
	for _, v := range in {
		out = append(out, v2.Volume(v))
	}
	return out
}
