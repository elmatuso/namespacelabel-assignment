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
	"math/rand"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	namespacelabelv1alpha1 "namespacelabel.dana.io/api/v1alpha1"
)

var rng = rand.New(rand.NewSource(time.Now().UnixNano()))

func randString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rng.Intn(len(letters))]
	}
	return string(b)
}

var _ = Describe("NamespaceLabel Controller", func() {
	var testNamespace string
	ctx := context.Background()

	BeforeEach(func() {
		testNamespace = "test-ns-" + randString(5)
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
	})

	AfterEach(func() {
		// Cleanup: delete all NamespaceLabel in the test namespace
		nlList := &namespacelabelv1alpha1.NamespaceLabelList{}
		Expect(k8sClient.List(ctx, nlList, client.InNamespace(testNamespace))).To(Succeed())
		for _, nl := range nlList.Items {
			Expect(k8sClient.Delete(ctx, &nl)).To(Succeed())
		}
		// Delete namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
	})

	It("should sync labels from a single NamespaceLabel to the Namespace", func() {
		nl := &namespacelabelv1alpha1.NamespaceLabel{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "nl1",
				Namespace: testNamespace,
			},
			Spec: namespacelabelv1alpha1.NamespaceLabelSpec{
				Labels: map[string]string{
					"label1": "value1",
					"label2": "value2",
				},
			},
		}
		Expect(k8sClient.Create(ctx, nl)).To(Succeed())

		reconciler := &NamespaceLabelReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}

		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "nl1", Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		ns := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testNamespace}, ns)).To(Succeed())
		Expect(ns.Labels["label1"]).To(Equal("value1"))
		Expect(ns.Labels["label2"]).To(Equal("value2"))
		Expect(ns.Annotations[ManagedLabelsAnnotation]).To(Equal("label1,label2"))
	})

	It("should merge labels from multiple NamespaceLabel objects and resolve conflicts (smallest name wins)", func() {
		nl1 := &namespacelabelv1alpha1.NamespaceLabel{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "a-first",
				Namespace: testNamespace,
			},
			Spec: namespacelabelv1alpha1.NamespaceLabelSpec{
				Labels: map[string]string{
					"conflict": "i-win",
					"unique1":  "v1",
				},
			},
		}
		nl2 := &namespacelabelv1alpha1.NamespaceLabel{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "b-second",
				Namespace: testNamespace,
			},
			Spec: namespacelabelv1alpha1.NamespaceLabelSpec{
				Labels: map[string]string{
					"conflict": "i-lose",
					"unique2":  "v2",
				},
			},
		}
		Expect(k8sClient.Create(ctx, nl1)).To(Succeed())
		Expect(k8sClient.Create(ctx, nl2)).To(Succeed())

		reconciler := &NamespaceLabelReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}

		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "b-second", Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		ns := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testNamespace}, ns)).To(Succeed())
		Expect(ns.Labels["conflict"]).To(Equal("i-win"))
		Expect(ns.Labels["unique1"]).To(Equal("v1"))
		Expect(ns.Labels["unique2"]).To(Equal("v2"))
		Expect(ns.Annotations[ManagedLabelsAnnotation]).To(ContainSubstring("conflict"))
		Expect(ns.Annotations[ManagedLabelsAnnotation]).To(ContainSubstring("unique1"))
		Expect(ns.Annotations[ManagedLabelsAnnotation]).To(ContainSubstring("unique2"))
	})

	It("should protect system labels with kubernetes.io/ or k8s.io/ prefixes", func() {
		ns := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testNamespace}, ns)).To(Succeed())
		if ns.Labels == nil {
			ns.Labels = make(map[string]string)
		}
		ns.Labels["kubernetes.io/metadata.name"] = testNamespace
		ns.Labels["k8s.io/something"] = "protected"
		Expect(k8sClient.Update(ctx, ns)).To(Succeed())

		nl := &namespacelabelv1alpha1.NamespaceLabel{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "nl-protected",
				Namespace: testNamespace,
			},
			Spec: namespacelabelv1alpha1.NamespaceLabelSpec{
				Labels: map[string]string{
					"kubernetes.io/metadata.name": "try-to-change",
					"normal":                      "value",
				},
			},
		}
		Expect(k8sClient.Create(ctx, nl)).To(Succeed())

		reconciler := &NamespaceLabelReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}

		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "nl-protected", Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testNamespace}, ns)).To(Succeed())
		Expect(ns.Labels["kubernetes.io/metadata.name"]).To(Equal(testNamespace)) // Unchanged
		Expect(ns.Labels["k8s.io/something"]).To(Equal("protected"))              // Unchanged
		Expect(ns.Labels["normal"]).To(Equal("value"))
	})

	It("should remove labels from the Namespace when a NamespaceLabel is deleted", func() {
		nl := &namespacelabelv1alpha1.NamespaceLabel{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "to-be-deleted",
				Namespace: testNamespace,
			},
			Spec: namespacelabelv1alpha1.NamespaceLabelSpec{
				Labels: map[string]string{
					"temp": "val",
				},
			},
		}
		Expect(k8sClient.Create(ctx, nl)).To(Succeed())

		reconciler := &NamespaceLabelReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}

		// Sync first
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "to-be-deleted", Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		ns := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testNamespace}, ns)).To(Succeed())
		Expect(ns.Labels["temp"]).To(Equal("val"))

		// Delete
		Expect(k8sClient.Delete(ctx, nl)).To(Succeed())

		// Reconcile again (the request for the deleted object)
		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "to-be-deleted", Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testNamespace}, ns)).To(Succeed())
		_, ok := ns.Labels["temp"]
		Expect(ok).To(BeFalse())
		Expect(ns.Annotations[ManagedLabelsAnnotation]).To(Equal(""))
	})
})
