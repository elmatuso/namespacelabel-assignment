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

// Package labels provides utility functions for label manipulation and validation.
package labels

import (
	"sort"
	"strings"

	namespacelabelv1alpha1 "namespacelabel.dana.io/api/v1alpha1"
)

// CalculateDesiredLabels merges labels from all NamespaceLabel objects.
// It uses a deterministic sort order (by name descending) to handle conflicts,
// so that a CR named "a" takes precedence over one named "z".
func CalculateDesiredLabels(items []namespacelabelv1alpha1.NamespaceLabel) map[string]string {
	// Sort items by name in descending order (e.g., "z", "b", "a").
	// Since "a" comes last in a descending sort, its labels will overwrite
	// any labels set by "b" or "z" when we iterate through the list.
	sortedItems := make([]namespacelabelv1alpha1.NamespaceLabel, len(items))
	copy(sortedItems, items)

	sort.Slice(sortedItems, func(i, j int) bool {
		return sortedItems[i].Name > sortedItems[j].Name
	})

	desired := make(map[string]string)
	for _, item := range sortedItems {
		for k, v := range item.Spec.Labels {
			desired[k] = v
		}
	}
	return desired
}

// IsProtected checks if a label key starts with any of the protected prefixes.
func IsProtected(key string, protectedPrefixes []string) bool {
	for _, p := range protectedPrefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

// ParseManagedLabels parses a comma-separated string of managed label keys into a set.
func ParseManagedLabels(annotation string) map[string]struct{} {
	result := make(map[string]struct{})
	if annotation == "" {
		return result
	}
	for _, k := range strings.Split(annotation, ",") {
		if k != "" {
			result[k] = struct{}{}
		}
	}
	return result
}

// FormatManagedLabels formats a slice of label keys into a sorted, comma-separated string.
func FormatManagedLabels(keys []string) string {
	sorted := make([]string, len(keys))
	copy(sorted, keys)
	sort.Strings(sorted)
	return strings.Join(sorted, ",")
}

// FailedLabel represents a label that could not be applied.
type FailedLabel struct {
	Key    string
	Reason string
}

// ClassifyLabels separates labels into applied and failed based on protection rules.
func ClassifyLabels(
	specLabels map[string]string,
	protectedPrefixes []string,
) (applied []string, failed []FailedLabel) {
	for k := range specLabels {
		if IsProtected(k, protectedPrefixes) {
			failed = append(failed, FailedLabel{
				Key:    k,
				Reason: "Label is protected (reserved prefix)",
			})
		} else {
			applied = append(applied, k)
		}
	}
	sort.Strings(applied)
	return applied, failed
}
