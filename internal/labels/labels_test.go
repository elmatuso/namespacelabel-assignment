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

package labels

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	namespacelabelv1alpha1 "namespacelabel.dana.io/api/v1alpha1"
)

func TestCalculateDesiredLabels(t *testing.T) {
	tests := []struct {
		name     string
		items    []namespacelabelv1alpha1.NamespaceLabel
		expected map[string]string
	}{
		{
			name:     "empty list",
			items:    []namespacelabelv1alpha1.NamespaceLabel{},
			expected: map[string]string{},
		},
		{
			name: "single item",
			items: []namespacelabelv1alpha1.NamespaceLabel{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test"},
					Spec:       namespacelabelv1alpha1.NamespaceLabelSpec{Labels: map[string]string{"key": "value"}},
				},
			},
			expected: map[string]string{"key": "value"},
		},
		{
			name: "conflict resolution - 'a' wins over 'z'",
			items: []namespacelabelv1alpha1.NamespaceLabel{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "z-cr"},
					Spec:       namespacelabelv1alpha1.NamespaceLabelSpec{Labels: map[string]string{"conflict": "from-z"}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "a-cr"},
					Spec:       namespacelabelv1alpha1.NamespaceLabelSpec{Labels: map[string]string{"conflict": "from-a"}},
				},
			},
			expected: map[string]string{"conflict": "from-a"},
		},
		{
			name: "merge non-conflicting labels",
			items: []namespacelabelv1alpha1.NamespaceLabel{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "first"},
					Spec:       namespacelabelv1alpha1.NamespaceLabelSpec{Labels: map[string]string{"key1": "val1"}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "second"},
					Spec:       namespacelabelv1alpha1.NamespaceLabelSpec{Labels: map[string]string{"key2": "val2"}},
				},
			},
			expected: map[string]string{"key1": "val1", "key2": "val2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateDesiredLabels(tt.items)
			if len(result) != len(tt.expected) {
				t.Errorf("expected %d labels, got %d", len(tt.expected), len(result))
			}
			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("expected label %s=%s, got %s", k, v, result[k])
				}
			}
		})
	}
}

func TestIsProtected(t *testing.T) {
	prefixes := []string{"kubernetes.io/", "k8s.io/"}

	tests := []struct {
		key      string
		expected bool
	}{
		{"kubernetes.io/name", true},
		{"k8s.io/component", true},
		{"app", false},
		{"my-label", false},
		{"kubernetes.io", false}, // no trailing slash in key
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := IsProtected(tt.key, prefixes); got != tt.expected {
				t.Errorf("IsProtected(%q) = %v, want %v", tt.key, got, tt.expected)
			}
		})
	}
}

func TestParseManagedLabels(t *testing.T) {
	tests := []struct {
		input    string
		expected map[string]struct{}
	}{
		{"", map[string]struct{}{}},
		{"a", map[string]struct{}{"a": {}}},
		{"a,b,c", map[string]struct{}{"a": {}, "b": {}, "c": {}}},
		{"a,,b", map[string]struct{}{"a": {}, "b": {}}}, // handles empty strings
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ParseManagedLabels(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("expected %d keys, got %d", len(tt.expected), len(result))
			}
			for k := range tt.expected {
				if _, ok := result[k]; !ok {
					t.Errorf("expected key %q to be present", k)
				}
			}
		})
	}
}

func TestFormatManagedLabels(t *testing.T) {
	tests := []struct {
		input    []string
		expected string
	}{
		{[]string{}, ""},
		{[]string{"a"}, "a"},
		{[]string{"c", "a", "b"}, "a,b,c"}, // should be sorted
		{[]string{"z", "a"}, "a,z"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := FormatManagedLabels(tt.input); got != tt.expected {
				t.Errorf("FormatManagedLabels(%v) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
