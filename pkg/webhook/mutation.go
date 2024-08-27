/*
Copyright 2024 Canonical, Ltd.

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

package webhook

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/appscode/jsonpatch"
	"k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	pebbleEnvVarName         string = "PEBBLE"
	pebbleEnvCopyOnceVarName string = "PEBBLE_COPY_ONCE"
	pebbleDefaultPath        string = "/var/lib/pebble/default"
	pebbleWritableSubpath    string = "writable"

	// Patch paths.
	containerVolumeMountPath string = "/spec/containers/%d/volumeMounts/-"
	containerEnvPath         string = "/spec/containers/%d/env%s"
	podVolumePath            string = "/spec/volumes/-"
)

func getContainerEnvPatchOps(container corev1.Container, currentPath, mountPath string, containerIndex int) []jsonpatch.Operation {
	envs := []corev1.EnvVar{
		{
			Name:  pebbleEnvVarName,
			Value: mountPath,
		},
		{
			Name:  pebbleEnvCopyOnceVarName,
			Value: currentPath,
		},
	}

	// If the container has no Env at all, we need to create it as well.
	if container.Env == nil {
		patchPath := fmt.Sprintf(containerEnvPath, containerIndex, "")
		return []jsonpatch.Operation{jsonpatch.NewPatch("add", patchPath, envs)}
	}

	ops := []jsonpatch.Operation{}

	for _, env := range envs {
		if index := findEnvVar(container, env.Name); index != -1 {
			patchPath := fmt.Sprintf(containerEnvPath, containerIndex, fmt.Sprintf("/%d", index))
			ops = append(ops, jsonpatch.NewPatch("replace", patchPath, env))
			continue
		}

		// The env var doesn't exist, add it.
		patchPath := fmt.Sprintf(containerEnvPath, containerIndex, "/-")
		ops = append(ops, jsonpatch.NewPatch("add", patchPath, env))
	}

	return ops
}

func containerHasMountPath(container corev1.Container, path string) bool {
	if container.VolumeMounts == nil {
		return false
	}

	for _, mount := range container.VolumeMounts {
		if mount.MountPath == path {
			return true
		}
	}

	return false
}

func findEnvVar(container corev1.Container, varName string) int {
	if container.Env == nil {
		return -1
	}

	for i, env := range container.Env {
		if env.Name == varName {
			return i
		}
	}

	return -1
}

// Get the configured $PEBBLE path env variable, if any. If not, return the default $PEBBLE path.
func getContainerPebblePath(container corev1.Container) string {
	if index := findEnvVar(container, pebbleEnvVarName); index != -1 {
		return container.Env[index].Value
	}

	return pebbleDefaultPath
}

func containerNeedsPebbleVolume(container corev1.Container) bool {
	// By default, Containers do not have read-only Root FS.
	secContext := container.SecurityContext
	if secContext == nil || secContext.ReadOnlyRootFilesystem == nil {
		return false
	}

	return *secContext.ReadOnlyRootFilesystem
}

// Returns the Pod JSON patches needed by any rock images in it.
// Pebble needs to be able to write its state. Thus, for containers with read-only root FS,
// the containers need an empty dir volume mount in their $PEBBLE folder.
func getPebbleVolumeMountPatches(pod *corev1.Pod) []jsonpatch.Operation {
	patches := []jsonpatch.Operation{}

	for i, container := range pod.Spec.Containers {
		// We don't need to mount a volume if the root FS is not read-only.
		if !containerNeedsPebbleVolume(container) {
			continue
		}

		// Make sure that there's no volume already targeting the $PEBBLE path.
		// If there is, we'll let the user handle it.
		// We might want to check if the user also defined the PEBBLE_COPY_ONCE env variable
		// in this case, as the $PEBBLE folder won't have the layers folder needed by Pebble.
		pebblePath := getContainerPebblePath(container)
		if containerHasMountPath(container, pebblePath) {
			continue
		}

		// The layers folder exists in the $PEBBLE path. This means we can't mount there, as
		// that will cause the layers folder to no longer exists in the $PEBBLE path.
		// Instead, we should mount in a subfolder, and set the $PEBBLE and $PEBBLE_READ_ONCE env vars.
		mountPath := filepath.Join(pebblePath, pebbleWritableSubpath)

		// Add volume patch to the current container.
		// The subpath is required if there are multiple rocks in the same Pod.
		patches = append(patches, jsonpatch.NewPatch("add", fmt.Sprintf(containerVolumeMountPath, i),
			corev1.VolumeMount{
				Name:      "pebble-dir",
				MountPath: mountPath,
				SubPath:   container.Name,
			},
		))

		// We're adding the same volume mount to all the containers in the Pod.
		// If we have multiple rocks in the same Pod, and they have the same $PEBBLE path,
		// they'd end up using the same socket and state files. We have to prevent that.
		patches = append(patches, getContainerEnvPatchOps(container, pebblePath, mountPath, i)...)
	}

	// If we don't have any volume mounts, we don't need to add any volume.
	if len(patches) == 0 {
		return patches
	}

	// Add patch for Pebble volume.
	patches = append(patches, jsonpatch.NewPatch("add", podVolumePath,
		corev1.Volume{
			Name:         "pebble-dir",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		},
	))

	return patches
}

func getPod(ar *v1.AdmissionReview) (*corev1.Pod, error) {
	pod := corev1.Pod{}

	if _, _, err := deserializer.Decode(ar.Request.Object.Raw, nil, &pod); err != nil {
		return nil, fmt.Errorf("request could not be decoded: %w", err)
	}

	return &pod, nil
}

// Check if the given container has the given mount path.
// Add an empty dir volume for Pebble to store its state in.
func addPebbleMountMutation(ar *v1.AdmissionReview) (*v1.AdmissionResponse, error) {
	podResource := metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	if ar.Request.Resource != podResource {
		return nil, fmt.Errorf("expected resource to be: '%s', actual: '%s'", podResource, ar.Request.Resource)
	}

	pod, err := getPod(ar)
	if err != nil {
		return nil, fmt.Errorf("failed to get pod: %w", err)
	}

	reviewResponse := v1.AdmissionResponse{}
	reviewResponse.Allowed = true

	patches := getPebbleVolumeMountPatches(pod)
	if len(patches) == 0 {
		// no mounts were necessary, so we don't need to change anything about the Pod.
		return &reviewResponse, nil
	}

	patchBytes, err := json.Marshal(patches)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal patches: %w", err)
	}

	pt := v1.PatchTypeJSONPatch
	reviewResponse.PatchType = &pt
	reviewResponse.Patch = patchBytes

	return &reviewResponse, nil
}
