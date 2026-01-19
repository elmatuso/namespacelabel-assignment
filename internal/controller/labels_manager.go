/*
Copyright 2025.

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

package controller

import (
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	namespacelabelv1alpha1 "namespacelabel.dana.io/api/v1alpha1"
)

// syncNamespaceLabels calculates and applies the desired labels to the namespace.
// It returns true if the namespace object was modified.
func syncNamespaceLabels(ns *corev1.Namespace, nlList []namespacelabelv1alpha1.NamespaceLabel) bool {
	desiredLabels := calculateDesiredLabels(nlList)

	// Get currently managed labels from annotation
	managedLabelsStr := ns.Annotations[ManagedLabelsAnnotation]
	managedLabelsKeys := make(map[string]struct{})
	if managedLabelsStr != "" {
		for _, k := range strings.Split(managedLabelsStr, ",") {
			managedLabelsKeys[k] = struct{}{}
		}
	}

	changed := false
	if ns.Labels == nil {
		ns.Labels = make(map[string]string)
	}

	// 1. Remove labels that are no longer managed
	for k := range managedLabelsKeys {
		if _, ok := desiredLabels[k]; !ok {
			if !isProtected(k) {
				delete(ns.Labels, k)
				changed = true
			}
		}
	}

	// 2. Add/Update desired labels
	newManagedKeys := []string{}
	for k, v := range desiredLabels {
		if !isProtected(k) {
			if ns.Labels[k] != v {
				ns.Labels[k] = v
				changed = true
			}
			newManagedKeys = append(newManagedKeys, k)
		}
	}

	// Update annotation
	sort.Strings(newManagedKeys)
	newManagedLabelsStr := strings.Join(newManagedKeys, ",")
	if ns.Annotations == nil {
		ns.Annotations = make(map[string]string)
	}
	if ns.Annotations[ManagedLabelsAnnotation] != newManagedLabelsStr {
		ns.Annotations[ManagedLabelsAnnotation] = newManagedLabelsStr
		changed = true
	}

	return changed
}

// calculateDesiredLabels merges labels from all NamespaceLabel CRs.
// Conflict resolution: Smallest name wins.
func calculateDesiredLabels(items []namespacelabelv1alpha1.NamespaceLabel) map[string]string {
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name > items[j].Name
	})

	desired := make(map[string]string)
	for _, item := range items {
		for k, v := range item.Spec.Labels {
			desired[k] = v
		}
	}
	return desired
}
