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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	namespacelabelv1alpha1 "namespacelabel.dana.io/api/v1alpha1"
	"namespacelabel.dana.io/internal/labels"
)

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
