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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	namespacelabelv1alpha1 "namespacelabel.dana.io/api/v1alpha1"
)

var _ = Describe("NamespaceLabel Controller", func() {
	const (
		managedLabelsAnnotation = "namespacelabel.dana.io/managed-labels"
	)

	var (
		ctx        = context.Background()
		reconciler *NamespaceLabelReconciler
	)

	BeforeEach(func() {
		reconciler = &NamespaceLabelReconciler{
			Client:                  k8sClient,
			Scheme:                  k8sClient.Scheme(),
			ProtectedPrefixes:       []string{"kubernetes.io/", "k8s.io/"},
			ManagedLabelsAnnotation: managedLabelsAnnotation,
		}
	})

	Context("Basic Reconciliation", func() {
		const resourceName = "basic-test-nl"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			nl := &namespacelabelv1alpha1.NamespaceLabel{}
			err := k8sClient.Get(ctx, typeNamespacedName, nl)
			if err != nil && errors.IsNotFound(err) {
				resource := &namespacelabelv1alpha1.NamespaceLabel{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: namespacelabelv1alpha1.NamespaceLabelSpec{
						Labels: map[string]string{
							"app": "test-app",
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &namespacelabelv1alpha1.NamespaceLabel{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("applies labels to namespace on reconcile", func() {
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			ns := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "default"}, ns)).To(Succeed())
			Expect(ns.Labels).To(HaveKeyWithValue("app", "test-app"))
		})

		It("updates status with applied labels", func() {
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			nl := &namespacelabelv1alpha1.NamespaceLabel{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, nl)).To(Succeed())
			Expect(nl.Status.AppliedLabels).To(ContainElement("app"))
		})
	})

	Context("Protected Label Handling", func() {
		It("blocks labels with protected prefixes", func() {
			nl := &namespacelabelv1alpha1.NamespaceLabel{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "protected-test-nl",
					Namespace: "default",
				},
				Spec: namespacelabelv1alpha1.NamespaceLabelSpec{
					Labels: map[string]string{
						"safe-label":        "allowed",
						"kubernetes.io/foo": "blocked",
					},
				},
			}
			Expect(k8sClient.Create(ctx, nl)).To(Succeed())

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "protected-test-nl", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())

			updatedNL := &namespacelabelv1alpha1.NamespaceLabel{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "protected-test-nl", Namespace: "default"}, updatedNL)).To(Succeed())

			Expect(updatedNL.Status.AppliedLabels).To(ContainElement("safe-label"))
			Expect(updatedNL.Status.FailedLabels).To(HaveLen(1))
			Expect(updatedNL.Status.FailedLabels[0].Key).To(Equal("kubernetes.io/foo"))
			Expect(updatedNL.Status.FailedLabels[0].Reason).To(ContainSubstring("protected"))

			Expect(k8sClient.Delete(ctx, nl)).To(Succeed())
		})

		It("preserves existing protected labels on namespace", func() {
			const nsName = "protected-ns-test"

			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
					Labels: map[string]string{
						"kubernetes.io/metadata.name": nsName,
						"k8s.io/important":            "do-not-remove",
					},
				},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())

			nl := &namespacelabelv1alpha1.NamespaceLabel{
				ObjectMeta: metav1.ObjectMeta{Name: "nl-in-protected-ns", Namespace: nsName},
				Spec: namespacelabelv1alpha1.NamespaceLabelSpec{
					Labels: map[string]string{"user-label": "user-value"},
				},
			}
			Expect(k8sClient.Create(ctx, nl)).To(Succeed())

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "nl-in-protected-ns", Namespace: nsName},
			})
			Expect(err).NotTo(HaveOccurred())

			updatedNS := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, updatedNS)).To(Succeed())
			Expect(updatedNS.Labels).To(HaveKeyWithValue("kubernetes.io/metadata.name", nsName))
			Expect(updatedNS.Labels).To(HaveKeyWithValue("k8s.io/important", "do-not-remove"))
			Expect(updatedNS.Labels).To(HaveKeyWithValue("user-label", "user-value"))

			Expect(k8sClient.Delete(ctx, nl)).To(Succeed())
			Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
		})
	})

	Context("NamespaceLabel Lifecycle", func() {
		It("adds labels to namespace when NamespaceLabel is created", func() {
			const nsName = "lifecycle-create-ns"

			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: nsName},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())

			// Verify namespace has no custom labels initially
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, ns)).To(Succeed())
			Expect(ns.Labels).NotTo(HaveKey("environment"))

			// Create NamespaceLabel
			nl := &namespacelabelv1alpha1.NamespaceLabel{
				ObjectMeta: metav1.ObjectMeta{Name: "env-label", Namespace: nsName},
				Spec: namespacelabelv1alpha1.NamespaceLabelSpec{
					Labels: map[string]string{"environment": "production"},
				},
			}
			Expect(k8sClient.Create(ctx, nl)).To(Succeed())

			// Reconcile
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "env-label", Namespace: nsName},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify label was added
			updatedNS := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, updatedNS)).To(Succeed())
			Expect(updatedNS.Labels).To(HaveKeyWithValue("environment", "production"))

			// Cleanup
			Expect(k8sClient.Delete(ctx, nl)).To(Succeed())
			Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
		})

		It("adds multiple labels from a single NamespaceLabel", func() {
			const nsName = "lifecycle-multi-label-ns"

			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: nsName},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())

			nl := &namespacelabelv1alpha1.NamespaceLabel{
				ObjectMeta: metav1.ObjectMeta{Name: "multi-label-nl", Namespace: nsName},
				Spec: namespacelabelv1alpha1.NamespaceLabelSpec{
					Labels: map[string]string{
						"team":        "platform",
						"cost-center": "engineering",
						"tier":        "backend",
					},
				},
			}
			Expect(k8sClient.Create(ctx, nl)).To(Succeed())

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "multi-label-nl", Namespace: nsName},
			})
			Expect(err).NotTo(HaveOccurred())

			updatedNS := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, updatedNS)).To(Succeed())
			Expect(updatedNS.Labels).To(HaveKeyWithValue("team", "platform"))
			Expect(updatedNS.Labels).To(HaveKeyWithValue("cost-center", "engineering"))
			Expect(updatedNS.Labels).To(HaveKeyWithValue("tier", "backend"))

			// Verify status shows all applied labels
			updatedNL := &namespacelabelv1alpha1.NamespaceLabel{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "multi-label-nl", Namespace: nsName}, updatedNL)).To(Succeed())
			Expect(updatedNL.Status.AppliedLabels).To(HaveLen(3))
			Expect(updatedNL.Status.AppliedLabels).To(ContainElements("team", "cost-center", "tier"))

			Expect(k8sClient.Delete(ctx, nl)).To(Succeed())
			Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
		})

		It("updates namespace labels when NamespaceLabel spec changes", func() {
			const nsName = "lifecycle-update-ns"

			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: nsName},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())

			nl := &namespacelabelv1alpha1.NamespaceLabel{
				ObjectMeta: metav1.ObjectMeta{Name: "updatable-nl", Namespace: nsName},
				Spec: namespacelabelv1alpha1.NamespaceLabelSpec{
					Labels: map[string]string{"version": "v1"},
				},
			}
			Expect(k8sClient.Create(ctx, nl)).To(Succeed())

			// First reconcile
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "updatable-nl", Namespace: nsName},
			})
			Expect(err).NotTo(HaveOccurred())

			updatedNS := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, updatedNS)).To(Succeed())
			Expect(updatedNS.Labels).To(HaveKeyWithValue("version", "v1"))

			// Update the NamespaceLabel
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "updatable-nl", Namespace: nsName}, nl)).To(Succeed())
			nl.Spec.Labels["version"] = "v2"
			nl.Spec.Labels["new-label"] = "added"
			Expect(k8sClient.Update(ctx, nl)).To(Succeed())

			// Second reconcile
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "updatable-nl", Namespace: nsName},
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, updatedNS)).To(Succeed())
			Expect(updatedNS.Labels).To(HaveKeyWithValue("version", "v2"))
			Expect(updatedNS.Labels).To(HaveKeyWithValue("new-label", "added"))

			Expect(k8sClient.Delete(ctx, nl)).To(Succeed())
			Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
		})

		It("removes labels from namespace when removed from NamespaceLabel spec", func() {
			const nsName = "lifecycle-remove-label-ns"

			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: nsName},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())

			nl := &namespacelabelv1alpha1.NamespaceLabel{
				ObjectMeta: metav1.ObjectMeta{Name: "shrinking-nl", Namespace: nsName},
				Spec: namespacelabelv1alpha1.NamespaceLabelSpec{
					Labels: map[string]string{
						"keep-me":   "yes",
						"remove-me": "soon",
					},
				},
			}
			Expect(k8sClient.Create(ctx, nl)).To(Succeed())

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "shrinking-nl", Namespace: nsName},
			})
			Expect(err).NotTo(HaveOccurred())

			updatedNS := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, updatedNS)).To(Succeed())
			Expect(updatedNS.Labels).To(HaveKey("remove-me"))

			// Remove one label from spec
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "shrinking-nl", Namespace: nsName}, nl)).To(Succeed())
			delete(nl.Spec.Labels, "remove-me")
			Expect(k8sClient.Update(ctx, nl)).To(Succeed())

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "shrinking-nl", Namespace: nsName},
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, updatedNS)).To(Succeed())
			Expect(updatedNS.Labels).To(HaveKeyWithValue("keep-me", "yes"))
			Expect(updatedNS.Labels).NotTo(HaveKey("remove-me"))

			Expect(k8sClient.Delete(ctx, nl)).To(Succeed())
			Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
		})

		It("handles reconcile for non-existent NamespaceLabel gracefully", func() {
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "does-not-exist", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("Multi-Resource Conflict Resolution", func() {
		It("merges labels from multiple NamespaceLabel resources", func() {
			const nsName = "merge-test-ns"

			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   nsName,
					Labels: map[string]string{"pre-existing": "keep-me"},
				},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())

			nlFirst := &namespacelabelv1alpha1.NamespaceLabel{
				ObjectMeta: metav1.ObjectMeta{Name: "first-nl", Namespace: nsName},
				Spec: namespacelabelv1alpha1.NamespaceLabelSpec{
					Labels: map[string]string{"from-first": "value1"},
				},
			}
			nlSecond := &namespacelabelv1alpha1.NamespaceLabel{
				ObjectMeta: metav1.ObjectMeta{Name: "second-nl", Namespace: nsName},
				Spec: namespacelabelv1alpha1.NamespaceLabelSpec{
					Labels: map[string]string{"from-second": "value2"},
				},
			}
			Expect(k8sClient.Create(ctx, nlFirst)).To(Succeed())
			Expect(k8sClient.Create(ctx, nlSecond)).To(Succeed())

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "first-nl", Namespace: nsName},
			})
			Expect(err).NotTo(HaveOccurred())

			updatedNS := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, updatedNS)).To(Succeed())
			Expect(updatedNS.Labels).To(HaveKeyWithValue("from-first", "value1"))
			Expect(updatedNS.Labels).To(HaveKeyWithValue("from-second", "value2"))
			Expect(updatedNS.Labels).To(HaveKeyWithValue("pre-existing", "keep-me"))

			Expect(k8sClient.Delete(ctx, nlFirst)).To(Succeed())
			Expect(k8sClient.Delete(ctx, nlSecond)).To(Succeed())
			Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
		})

		It("resolves conflicts deterministically by resource name", func() {
			const nsName = "conflict-test-ns"

			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: nsName},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())

			// Resources sorted by name descending: z-nl comes before a-nl
			// So a-nl (processed last) wins conflicts
			nlA := &namespacelabelv1alpha1.NamespaceLabel{
				ObjectMeta: metav1.ObjectMeta{Name: "a-nl", Namespace: nsName},
				Spec: namespacelabelv1alpha1.NamespaceLabelSpec{
					Labels: map[string]string{"conflict-key": "from-a"},
				},
			}
			nlZ := &namespacelabelv1alpha1.NamespaceLabel{
				ObjectMeta: metav1.ObjectMeta{Name: "z-nl", Namespace: nsName},
				Spec: namespacelabelv1alpha1.NamespaceLabelSpec{
					Labels: map[string]string{"conflict-key": "from-z", "unique-z": "only-z"},
				},
			}
			Expect(k8sClient.Create(ctx, nlA)).To(Succeed())
			Expect(k8sClient.Create(ctx, nlZ)).To(Succeed())

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "a-nl", Namespace: nsName},
			})
			Expect(err).NotTo(HaveOccurred())

			updatedNS := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, updatedNS)).To(Succeed())
			Expect(updatedNS.Labels["conflict-key"]).To(Equal("from-a"), "a-nl should win (sorted last)")
			Expect(updatedNS.Labels).To(HaveKeyWithValue("unique-z", "only-z"))

			Expect(k8sClient.Delete(ctx, nlA)).To(Succeed())
			Expect(k8sClient.Delete(ctx, nlZ)).To(Succeed())
			Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
		})

		It("removes stale labels when a NamespaceLabel is deleted", func() {
			const nsName = "stale-test-ns"

			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   nsName,
					Labels: map[string]string{"pre-existing": "keep-me"},
				},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())

			nl := &namespacelabelv1alpha1.NamespaceLabel{
				ObjectMeta: metav1.ObjectMeta{Name: "temp-nl", Namespace: nsName},
				Spec: namespacelabelv1alpha1.NamespaceLabelSpec{
					Labels: map[string]string{"temporary": "will-be-removed"},
				},
			}
			Expect(k8sClient.Create(ctx, nl)).To(Succeed())

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "temp-nl", Namespace: nsName},
			})
			Expect(err).NotTo(HaveOccurred())

			updatedNS := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, updatedNS)).To(Succeed())
			Expect(updatedNS.Labels).To(HaveKey("temporary"))

			Expect(k8sClient.Delete(ctx, nl)).To(Succeed())

			// Reconcile again (triggered by deletion)
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "temp-nl", Namespace: nsName},
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, updatedNS)).To(Succeed())
			Expect(updatedNS.Labels).NotTo(HaveKey("temporary"))
			Expect(updatedNS.Labels).To(HaveKeyWithValue("pre-existing", "keep-me"))

			Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
		})
	})
})
