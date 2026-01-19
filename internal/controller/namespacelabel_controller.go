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
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	namespacelabelv1alpha1 "namespacelabel.dana.io/api/v1alpha1"
)

const (
	// Condition types
	ConditionTypeApplied = "Applied"

	// Reasons
	ReasonSucceeded = "Succeeded"
	ReasonFailed    = "Failed"
)

// NamespaceLabelReconciler reconciles a NamespaceLabel object
type NamespaceLabelReconciler struct {
	client.Client
	Scheme                  *runtime.Scheme
	ProtectedPrefixes       []string
	ManagedLabelsAnnotation string
}

// +kubebuilder:rbac:groups=namespacelabel.dana.io,resources=namespacelabels,verbs=get;list;watch
// +kubebuilder:rbac:groups=namespacelabel.dana.io,resources=namespacelabels/status,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;patch

// Reconcile is the main reconciliation loop which aims to move the current state of the cluster closer to the desired state.
func (r *NamespaceLabelReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	var nlList namespacelabelv1alpha1.NamespaceLabelList
	if err := r.List(ctx, &nlList, client.InNamespace(req.Namespace)); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var ns corev1.Namespace
	if err := r.Get(ctx, client.ObjectKey{Name: req.Namespace}, &ns); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	nsOriginal := ns.DeepCopy()

	desiredLabels := calculateDesiredLabels(nlList.Items)

	managedLabelsStr := ns.Annotations[r.ManagedLabelsAnnotation]
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

	for k := range managedLabelsKeys {
		if _, ok := desiredLabels[k]; !ok {
			if !r.isProtected(k) {
				delete(ns.Labels, k)
				changed = true
			}
		}
	}

	newManagedKeys := []string{}
	for k, v := range desiredLabels {
		if !r.isProtected(k) {
			if ns.Labels[k] != v {
				ns.Labels[k] = v
				changed = true
			}
			newManagedKeys = append(newManagedKeys, k)
		}
	}

	sort.Strings(newManagedKeys)
	newManagedLabelsStr := strings.Join(newManagedKeys, ",")
	if ns.Annotations == nil {
		ns.Annotations = make(map[string]string)
	}
	if ns.Annotations[r.ManagedLabelsAnnotation] != newManagedLabelsStr {
		ns.Annotations[r.ManagedLabelsAnnotation] = newManagedLabelsStr
		changed = true
	}

	if changed {
		l.Info("Syncing labels to namespace", "namespace", ns.Name, "labels", newManagedLabelsStr)
		patch := client.MergeFrom(nsOriginal)
		if err := r.Patch(ctx, &ns, patch); err != nil {
			l.Error(err, "unable to patch Namespace labels")
			r.updateStatus(ctx, nlList.Items, metav1.ConditionFalse, ReasonFailed, "Failed to patch namespace")
			return ctrl.Result{}, err
		}
	}

	r.updateStatus(ctx, nlList.Items, metav1.ConditionTrue, ReasonSucceeded, "Labels synced successfully")
	return ctrl.Result{}, nil
}

// updateStatus updates the status of each NamespaceLabel object in the provided list.
func (r *NamespaceLabelReconciler) updateStatus(ctx context.Context, items []namespacelabelv1alpha1.NamespaceLabel, status metav1.ConditionStatus, reason, message string) {
	l := log.FromContext(ctx)
	for _, item := range items {
		// Calculate applied and failed labels for this specific CR
		var appliedLabels []string
		var failedLabels []namespacelabelv1alpha1.FailedLabel

		for k := range item.Spec.Labels {
			if r.isProtected(k) {
				failedLabels = append(failedLabels, namespacelabelv1alpha1.FailedLabel{
					Key:    k,
					Reason: "Label is protected (reserved prefix)",
				})
			} else {
				appliedLabels = append(appliedLabels, k)
			}
		}
		sort.Strings(appliedLabels)

		item.Status.AppliedLabels = appliedLabels
		item.Status.FailedLabels = failedLabels

		meta.SetStatusCondition(&item.Status.Conditions, metav1.Condition{
			Type:    ConditionTypeApplied,
			Status:  status,
			Reason:  reason,
			Message: message,
		})

		if err := r.Status().Update(ctx, &item); err != nil {
			l.Error(err, "unable to update NamespaceLabel status", "name", item.Name)
		}
	}
}

// calculateDesiredLabels merges labels from all NamespaceLabel objects in a namespace.
// It uses a deterministic sort order to handle conflicts.
func calculateDesiredLabels(items []namespacelabelv1alpha1.NamespaceLabel) map[string]string {
	// Sort items by name in descending order (e.g., "z", "b", "a").
	// Since "a" comes last in a descending sort, its labels will overwrite
	// any labels set by "b" or "z" when we iterate through the list.
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

// isProtected checks if a label key starts with any of the protected prefixes.
func (r *NamespaceLabelReconciler) isProtected(key string) bool {
	for _, p := range r.ProtectedPrefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceLabelReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&namespacelabelv1alpha1.NamespaceLabel{}).
		Watches(
			&corev1.Namespace{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				var nlList namespacelabelv1alpha1.NamespaceLabelList
				if err := r.List(ctx, &nlList, client.InNamespace(obj.GetName())); err != nil {
					return nil
				}
				var requests []reconcile.Request
				for _, nl := range nlList.Items {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name:      nl.Name,
							Namespace: nl.Namespace,
						},
					})
				}
				return requests
			}),
		).
		Complete(r)
}
