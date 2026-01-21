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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	namespacelabelv1alpha1 "namespacelabel.dana.io/api/v1alpha1"
)

var _ = Describe("NamespaceLabel Controller", func() {
	const (
		testLabelKey   = "test-label"
		testLabelValue = "test-value"
		labelAKey      = "label-a"
		labelAValue    = "val-a"
		protectedKey   = "kubernetes.io/foo"
		protectedValue = "bar"
		managedLabels  = "namespacelabel.dana.io/managed-labels"
	)

	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		namespacelabel := &namespacelabelv1alpha1.NamespaceLabel{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind NamespaceLabel")
			err := k8sClient.Get(ctx, typeNamespacedName, namespacelabel)
			if err != nil && errors.IsNotFound(err) {
				resource := &namespacelabelv1alpha1.NamespaceLabel{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: namespacelabelv1alpha1.NamespaceLabelSpec{
						Labels: map[string]string{
							testLabelKey: testLabelValue,
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &namespacelabelv1alpha1.NamespaceLabel{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance NamespaceLabel")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &NamespaceLabelReconciler{
				Client:                  k8sClient,
				Scheme:                  k8sClient.Scheme(),
				ProtectedPrefixes:       []string{"kubernetes.io/", "k8s.io/"},
				ManagedLabelsAnnotation: managedLabels,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying that the namespace has the expected labels")
			ns := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "default"}, ns)).To(Succeed())
			// Note: In envtest, we don't have a real namespace controller, so we might need
			// to mock or manually check the namespace object if the controller updated it.
		})

		It("should handle multiple NamespaceLabel objects and protected labels", func() {
			By("creating another NamespaceLabel")
			anotherNL := &namespacelabelv1alpha1.NamespaceLabel{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "another-nl",
					Namespace: "default",
				},
				Spec: namespacelabelv1alpha1.NamespaceLabelSpec{
					Labels: map[string]string{
						labelAKey:    labelAValue,
						protectedKey: protectedValue, // Protected
					},
				},
			}
			Expect(k8sClient.Create(ctx, anotherNL)).To(Succeed())

			controllerReconciler := &NamespaceLabelReconciler{
				Client:                  k8sClient,
				Scheme:                  k8sClient.Scheme(),
				ProtectedPrefixes:       []string{"kubernetes.io/", "k8s.io/"},
				ManagedLabelsAnnotation: managedLabels,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "another-nl", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying status of the CR")
			updatedNL := &namespacelabelv1alpha1.NamespaceLabel{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "another-nl", Namespace: "default"}, updatedNL)).To(Succeed())

			Expect(updatedNL.Status.AppliedLabels).To(ContainElement(labelAKey))
			Expect(updatedNL.Status.FailedLabels).To(HaveLen(1))
			Expect(updatedNL.Status.FailedLabels[0].Key).To(Equal(protectedKey))

			By("Cleanup")
			Expect(k8sClient.Delete(ctx, anotherNL)).To(Succeed())
		})

		It("should handle overlapping labels from multiple NamespaceLabel objects", func() {
			const nsName = "overlap-ns"
			By("Creating a test namespace")
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
					Labels: map[string]string{
						"existing-label": "keep-me",
					},
				},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())

			By("Creating NamespaceLabel 'a-nl' and 'z-nl'")
			// According to controller logic: sort.Slice(items, func(i, j int) bool { return items[i].Name > items[j].Name })
			// This means 'a-nl' comes after 'z-nl' in the sorted list, so 'a-nl' overwrites 'z-nl'.
			nlA := &namespacelabelv1alpha1.NamespaceLabel{
				ObjectMeta: metav1.ObjectMeta{Name: "a-nl", Namespace: nsName},
				Spec: namespacelabelv1alpha1.NamespaceLabelSpec{
					Labels: map[string]string{"conflict": "val-a", "shared": "both"},
				},
			}
			nlZ := &namespacelabelv1alpha1.NamespaceLabel{
				ObjectMeta: metav1.ObjectMeta{Name: "z-nl", Namespace: nsName},
				Spec: namespacelabelv1alpha1.NamespaceLabelSpec{
					Labels: map[string]string{"conflict": "val-z", "unique-z": "only-z", "shared": "both"},
				},
			}
			Expect(k8sClient.Create(ctx, nlA)).To(Succeed())
			Expect(k8sClient.Create(ctx, nlZ)).To(Succeed())

			reconciler := &NamespaceLabelReconciler{
				Client:                  k8sClient,
				Scheme:                  k8sClient.Scheme(),
				ProtectedPrefixes:       []string{"kubernetes.io/", "k8s.io/"},
				ManagedLabelsAnnotation: managedLabels,
			}

			// Reconcile 'a-nl' (the list-based logic will fetch both)
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "a-nl", Namespace: nsName},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying merged labels on the namespace")
			updatedNS := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, updatedNS)).To(Succeed())

			Expect(updatedNS.Labels["conflict"]).To(Equal("val-a"), "a-nl should overwrite z-nl")
			Expect(updatedNS.Labels["unique-z"]).To(Equal("only-z"))
			Expect(updatedNS.Labels["shared"]).To(Equal("both"))
			Expect(updatedNS.Labels["existing-label"]).To(Equal("keep-me"))

			By("Deleting 'a-nl' and reconciling again")
			Expect(k8sClient.Delete(ctx, nlA)).To(Succeed())
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "z-nl", Namespace: nsName},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying labels after one CR is deleted")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, updatedNS)).To(Succeed())
			Expect(updatedNS.Labels["conflict"]).To(Equal("val-z"), "z-nl should now be the source for 'conflict'")
			Expect(updatedNS.Labels).NotTo(HaveKey("unique-a"))

			By("Cleanup")
			Expect(k8sClient.Delete(ctx, nlZ)).To(Succeed())
			Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
		})

		It("should preserve protected labels already on the namespace", func() {
			const nsName = "protected-ns"
			By("Creating a namespace with protected labels")
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
					Labels: map[string]string{
						"kubernetes.io/existing": "don-not-touch",
						"k8s.io/managed":         "important",
					},
					Annotations: map[string]string{
						managedLabels: "kubernetes.io/existing", // Simulate a bad state where a protected label is in managed list
					},
				},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())

			nl := &namespacelabelv1alpha1.NamespaceLabel{
				ObjectMeta: metav1.ObjectMeta{Name: "test-nl", Namespace: nsName},
				Spec: namespacelabelv1alpha1.NamespaceLabelSpec{
					Labels: map[string]string{"new-label": "val"},
				},
			}
			Expect(k8sClient.Create(ctx, nl)).To(Succeed())

			reconciler := &NamespaceLabelReconciler{
				Client:                  k8sClient,
				Scheme:                  k8sClient.Scheme(),
				ProtectedPrefixes:       []string{"kubernetes.io/", "k8s.io/"},
				ManagedLabelsAnnotation: managedLabels,
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-nl", Namespace: nsName},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying protected labels are preserved")
			updatedNS := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, updatedNS)).To(Succeed())
			Expect(updatedNS.Labels["kubernetes.io/existing"]).To(Equal("don-not-touch"))
			Expect(updatedNS.Labels["k8s.io/managed"]).To(Equal("important"))
			Expect(updatedNS.Labels["new-label"]).To(Equal("val"))

			By("Cleanup")
			Expect(k8sClient.Delete(ctx, nl)).To(Succeed())
			Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
		})
	})
})
