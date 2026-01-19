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
	"context"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	namespacelabelv1alpha1 "namespacelabel.dana.io/api/v1alpha1"
	"namespacelabel.dana.io/internal/labels"
)

// fetchNamespaceLabels retrieves all NamespaceLabel CRs in the given namespace.
func (r *NamespaceLabelReconciler) fetchNamespaceLabels(
	ctx context.Context,
	namespace string,
) (*namespacelabelv1alpha1.NamespaceLabelList, error) {
	var nlList namespacelabelv1alpha1.NamespaceLabelList
	if err := r.List(ctx, &nlList, client.InNamespace(namespace)); err != nil {
		return nil, client.IgnoreNotFound(err)
	}
	return &nlList, nil
}

// fetchNamespace retrieves the Namespace object by name.
func (r *NamespaceLabelReconciler) fetchNamespace(ctx context.Context, name string) (*corev1.Namespace, error) {
	var ns corev1.Namespace
	if err := r.Get(ctx, client.ObjectKey{Name: name}, &ns); err != nil {
		return nil, client.IgnoreNotFound(err)
	}
	return &ns, nil
}

// syncLabels applies desired labels to the namespace and removes stale ones.
// Returns true if any changes were made.
func (r *NamespaceLabelReconciler) syncLabels(ns *corev1.Namespace, desiredLabels map[string]string) bool {
	if ns.Labels == nil {
		ns.Labels = make(map[string]string)
	}
	if ns.Annotations == nil {
		ns.Annotations = make(map[string]string)
	}

	managedKeys := labels.ParseManagedLabels(ns.Annotations[r.ManagedLabelsAnnotation])

	changed := r.removeStaleLabels(ns, managedKeys, desiredLabels)
	changed = r.applyDesiredLabels(ns, desiredLabels) || changed
	changed = r.updateManagedAnnotation(ns, desiredLabels) || changed

	return changed
}

// removeStaleLabels removes labels that were previously managed but are no longer desired.
func (r *NamespaceLabelReconciler) removeStaleLabels(
	ns *corev1.Namespace,
	managedKeys map[string]struct{},
	desiredLabels map[string]string,
) bool {
	changed := false
	for k := range managedKeys {
		if _, stillDesired := desiredLabels[k]; !stillDesired {
			if !labels.IsProtected(k, r.ProtectedPrefixes) {
				delete(ns.Labels, k)
				changed = true
			}
		}
	}
	return changed
}

// applyDesiredLabels adds or updates labels on the namespace.
func (r *NamespaceLabelReconciler) applyDesiredLabels(ns *corev1.Namespace, desiredLabels map[string]string) bool {
	changed := false
	for k, v := range desiredLabels {
		if !labels.IsProtected(k, r.ProtectedPrefixes) {
			if ns.Labels[k] != v {
				ns.Labels[k] = v
				changed = true
			}
		}
	}
	return changed
}

// updateManagedAnnotation updates the annotation tracking which labels are managed.
func (r *NamespaceLabelReconciler) updateManagedAnnotation(
	ns *corev1.Namespace,
	desiredLabels map[string]string,
) bool {
	var newManagedKeys []string
	for k := range desiredLabels {
		if !labels.IsProtected(k, r.ProtectedPrefixes) {
			newManagedKeys = append(newManagedKeys, k)
		}
	}

	newAnnotation := labels.FormatManagedLabels(newManagedKeys)
	if ns.Annotations[r.ManagedLabelsAnnotation] != newAnnotation {
		ns.Annotations[r.ManagedLabelsAnnotation] = newAnnotation
		return true
	}
	return false
}

// patchNamespace applies changes to the namespace using a merge patch.
func (r *NamespaceLabelReconciler) patchNamespace(
	ctx context.Context,
	ns *corev1.Namespace,
	original *corev1.Namespace,
) error {
	return r.Patch(ctx, ns, client.MergeFrom(original))
}
