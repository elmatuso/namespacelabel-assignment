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
