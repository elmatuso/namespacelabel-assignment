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
	"namespacelabel.dana.io/internal/labels"
)

const (
	ConditionTypeApplied = "Applied"
	ReasonSucceeded      = "Succeeded"
	ReasonFailed         = "Failed"
)

// NamespaceLabelReconciler reconciles a NamespaceLabel object.
type NamespaceLabelReconciler struct {
	client.Client
	Scheme                  *runtime.Scheme
	ProtectedPrefixes       []string
	ManagedLabelsAnnotation string
}

// +kubebuilder:rbac:groups=namespacelabel.dana.io,resources=namespacelabels,verbs=get;list;watch
// +kubebuilder:rbac:groups=namespacelabel.dana.io,resources=namespacelabels/status,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;patch

// Reconcile synchronizes labels from NamespaceLabel CRs to the parent Namespace.
func (r *NamespaceLabelReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	nlList, err := r.fetchNamespaceLabels(ctx, req.Namespace)
	if err != nil {
		return ctrl.Result{}, err
	}

	ns, err := r.fetchNamespace(ctx, req.Namespace)
	if err != nil {
		return ctrl.Result{}, err
	}

	nsOriginal := ns.DeepCopy()
	desiredLabels := labels.CalculateDesiredLabels(nlList.Items)

	changed := r.syncLabels(ns, desiredLabels)

	if changed {
		l.Info("Syncing labels to namespace", "namespace", ns.Name)
		if err := r.patchNamespace(ctx, ns, nsOriginal); err != nil {
			l.Error(err, "unable to patch Namespace labels")
			r.updateAllStatuses(ctx, nlList.Items, metav1.ConditionFalse, ReasonFailed, "Failed to patch namespace")
			return ctrl.Result{}, err
		}
	}

	r.updateAllStatuses(ctx, nlList.Items, metav1.ConditionTrue, ReasonSucceeded, "Labels synced successfully")
	return ctrl.Result{}, nil
}

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

// updateAllStatuses updates the status of all NamespaceLabel CRs.
func (r *NamespaceLabelReconciler) updateAllStatuses(
	ctx context.Context,
	items []namespacelabelv1alpha1.NamespaceLabel,
	status metav1.ConditionStatus,
	reason, message string,
) {
	l := log.FromContext(ctx)
	for i := range items {
		if err := r.updateSingleStatus(ctx, &items[i], status, reason, message); err != nil {
			l.Error(err, "unable to update NamespaceLabel status", "name", items[i].Name)
		}
	}
}

// updateSingleStatus updates the status of a single NamespaceLabel CR.
func (r *NamespaceLabelReconciler) updateSingleStatus(
	ctx context.Context,
	item *namespacelabelv1alpha1.NamespaceLabel,
	status metav1.ConditionStatus,
	reason, message string,
) error {
	appliedLabels, failedLabels := labels.ClassifyLabels(item.Spec.Labels, r.ProtectedPrefixes)

	item.Status.AppliedLabels = appliedLabels
	item.Status.FailedLabels = convertFailedLabels(failedLabels)

	meta.SetStatusCondition(&item.Status.Conditions, metav1.Condition{
		Type:    ConditionTypeApplied,
		Status:  status,
		Reason:  reason,
		Message: message,
	})

	return r.Status().Update(ctx, item)
}

// convertFailedLabels converts labels.FailedLabel to the API type.
func convertFailedLabels(failed []labels.FailedLabel) []namespacelabelv1alpha1.FailedLabel {
	result := make([]namespacelabelv1alpha1.FailedLabel, len(failed))
	for i, f := range failed {
		result[i] = namespacelabelv1alpha1.FailedLabel{
			Key:    f.Key,
			Reason: f.Reason,
		}
	}
	return result
}

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceLabelReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&namespacelabelv1alpha1.NamespaceLabel{}).
		Watches(
			&corev1.Namespace{},
			handler.EnqueueRequestsFromMapFunc(r.findNamespaceLabelsForNamespace),
		).
		Complete(r)
}

// findNamespaceLabelsForNamespace returns reconcile requests for all NamespaceLabel CRs in a namespace.
func (r *NamespaceLabelReconciler) findNamespaceLabelsForNamespace(
	ctx context.Context,
	obj client.Object,
) []reconcile.Request {
	var nlList namespacelabelv1alpha1.NamespaceLabelList
	if err := r.List(ctx, &nlList, client.InNamespace(obj.GetName())); err != nil {
		return nil
	}

	requests := make([]reconcile.Request, 0, len(nlList.Items))
	for _, nl := range nlList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      nl.Name,
				Namespace: nl.Namespace,
			},
		})
	}
	return requests
}
