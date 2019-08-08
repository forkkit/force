/*
Copyright 2019 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kube

import (
	"bytes"
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// diffPodSets returns a difference in Pods between existing and new.
// The difference contains all Pods from new not found in existing and
// all new Pods that differ from those in existing.
func diffPodSets(existing map[string]corev1.Pod, new map[string]corev1.Pod) []podDiff {
	var diffs []podDiff
	for podName := range new {
		newPod := new[podName]
		oldPod, exists := existing[podName]
		if !exists {
			diffs = append(diffs, diffPods(nil, &newPod))
		} else {
			diff := diffPods(&oldPod, &newPod)
			if !diff.isEmpty() {
				diffs = append(diffs, diff)
			}
		}

	}
	return diffs
}

// diffPods diffs status changes of two pods, old can be nil
func diffPods(old *corev1.Pod, new *corev1.Pod) podDiff {
	diff := podDiff{
		old: old,
		new: new,
	}
	if old != nil && old.Status.Phase != new.Status.Phase {
		diff.phase = &phaseDiff{
			old: old.Status.Phase,
			new: new.Status.Phase,
		}
	}
	diff.containers = diffStatuses(collectStatuses(old), collectStatuses(new))

	return diff
}

func collectStatuses(pod *corev1.Pod) map[string]corev1.ContainerStatus {
	out := make(map[string]corev1.ContainerStatus)
	if pod == nil {
		return out
	}
	for _, status := range pod.Status.ContainerStatuses {
		out[status.Name] = status
	}
	return out
}

func diffStatuses(old, new map[string]corev1.ContainerStatus) []containerDiff {
	var out []containerDiff
	for name := range new {
		newStatus := new[name]
		oldStatus, exists := old[name]
		if !exists {
			out = append(out, containerDiff{name: name, new: &newStatus})
		} else {
			if !containerStatesEqual(oldStatus.State, newStatus.State) || oldStatus.RestartCount != newStatus.RestartCount {
				out = append(out, containerDiff{name: name, new: &newStatus, old: &oldStatus})
			}
		}
	}
	return out
}

// containerStatesEqual returns true if container states are equal
func containerStatesEqual(a, b corev1.ContainerState) bool {
	return (a.Running != nil && b.Running != nil) ||
		(a.Terminated != nil && b.Terminated != nil) ||
		(a.Waiting != nil && b.Waiting != nil)
}

type podDiff struct {
	old        *corev1.Pod
	new        *corev1.Pod
	phase      *phaseDiff
	containers []containerDiff
}

func (p *podDiff) isAdded() bool {
	return p.old == nil
}

func (p *podDiff) isEmpty() bool {
	return p.phase == nil && !p.isAdded() && len(p.containers) == 0
}

func (p *podDiff) String() string {
	out := &bytes.Buffer{}
	if p.old == nil {
		fmt.Fprintf(out, "Created %v.\n", describe(p.new))
	}
	if p.phase != nil {
		fmt.Fprintf(out,
			"%v, has changed state from %q to %q.\n", describe(p.new), p.phase.old, p.phase.new)
	}
	for _, diff := range p.containers {
		fmt.Fprintln(out, diff.String())
	}
	return out.String()
}

type phaseDiff struct {
	old corev1.PodPhase
	new corev1.PodPhase
}

type containerDiff struct {
	name string
	old  *corev1.ContainerStatus
	new  *corev1.ContainerStatus
}

func (c *containerDiff) String() string {
	if c.old == nil {
		return fmt.Sprintf("Container %q created, current state is %q.", c.name, describeState(c.new.State))
	}
	if c.old.RestartCount != c.new.RestartCount {
		return fmt.Sprintf("Container %q restarted, current state is %q.", c.name, describeState(c.new.State))
	}
	return fmt.Sprintf("Container %q changed status from %q to %q.", c.name, describeState(c.old.State), describeState(c.new.State))
}

func describeState(s corev1.ContainerState) string {
	if s.Running != nil {
		return "running"
	}
	if s.Terminated != nil {
		return fmt.Sprintf("terminated, exit code %v", s.Terminated.ExitCode)
	}
	if s.Waiting != nil {
		return fmt.Sprintf("waiting, reason %v", s.Waiting.Reason)
	}
	return "unknown"
}
