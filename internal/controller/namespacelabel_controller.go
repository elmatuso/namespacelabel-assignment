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
	// ManagedLabelsAnnotation is the key used in the namespace annotations to track
	// which labels are managed by the NamespaceLabel operator.
	ManagedLabelsAnnotation = "namespacelabel.dana.io/managed-labels"
)

// NamespaceLabelReconciler reconciles a NamespaceLabel object
type NamespaceLabelReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=namespacelabel.dana.io,resources=namespacelabels,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=namespacelabel.dana.io,resources=namespacelabels/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=namespacelabel.dana.io,resources=namespacelabels/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;update;patch

// Reconcile calculates and applies the desired state of labels to the target namespace.
func (r *NamespaceLabelReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	// Fetch all NamespaceLabel objects in the namespace
	var nlList namespacelabelv1alpha1.NamespaceLabelList
	if err := r.List(ctx, &nlList, client.InNamespace(req.Namespace)); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Fetch the Namespace object
	var ns corev1.Namespace
	if err := r.Get(ctx, client.ObjectKey{Name: req.Namespace}, &ns); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Sync labels using the labels manager
	if changed := syncNamespaceLabels(&ns, nlList.Items); changed {
		l.Info("Syncing labels to namespace", "namespace", ns.Name, "managed-labels", ns.Annotations[ManagedLabelsAnnotation])
		if err := r.Update(ctx, &ns); err != nil {
			l.Error(err, "unable to update Namespace labels")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceLabelReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&namespacelabelv1alpha1.NamespaceLabel{}).
		Watches(
			&corev1.Namespace{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				// When a namespace changes, trigger reconciliation for all
				// NamespaceLabels in that namespace
				return []reconcile.Request{{NamespacedName: types.NamespacedName{
					Name:      "dummy", // The reconciler lists all CRs in the namespace, so name doesn't matter
					Namespace: obj.GetName(),
				}}}
			}),
		).
		Named("namespacelabel").
		Complete(r)
}
