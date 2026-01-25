//go:build e2e
// +build e2e

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

package e2e

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"namespacelabel.dana.io/test/utils"
)

// namespace where the project is deployed in
const namespace = "namespacelabel-assignment-system"

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	// Before running the tests, set up the environment by creating the namespace,
	// enforce the restricted security policy to the namespace, installing CRDs,
	// and deploying the controller.
	BeforeAll(func() {
		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("labeling the namespace to enforce the restricted security policy")
		cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
			"pod-security.kubernetes.io/enforce=restricted")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager")
		cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", projectImage))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")
	})

	// After all tests have been executed, clean up by undeploying the controller, uninstalling CRDs,
	// and deleting the namespace.
	AfterAll(func() {
		By("undeploying the controller-manager")
		cmd := exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace)
		_, _ = utils.Run(cmd)
	})

	// After each test, check for failures and collect logs, events,
	// and pod descriptions for debugging.
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}

			By("Fetching Kubernetes events")
			cmd = exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}

			By("Fetching controller manager pod description")
			cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", namespace)
			podDescription, err := utils.Run(cmd)
			if err == nil {
				fmt.Println("Pod description:\n", podDescription)
			} else {
				fmt.Println("Failed to describe controller pod")
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Manager", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				// Get the name of the controller-manager pod
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				// Validate the pod's status
				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status")
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})
	})

	Context("NamespaceLabel Creation", func() {
		const testNS = "e2e-creation-test"

		BeforeEach(func() {
			By("creating a test namespace")
			cmd := exec.Command("kubectl", "create", "ns", testNS)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			By("cleaning up test namespace")
			cmd := exec.Command("kubectl", "delete", "ns", testNS, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should apply labels to namespace when NamespaceLabel is created", func() {
			By("creating a NamespaceLabel CR")
			crYAML := fmt.Sprintf(`
apiVersion: namespacelabel.dana.io/v1alpha1
kind: NamespaceLabel
metadata:
  name: simple-labels
  namespace: %s
spec:
  labels:
    team: backend
    env: production
`, testNS)
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(crYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying labels are applied to the namespace")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "ns", testNS,
					"-o", "jsonpath={.metadata.labels}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				var labels map[string]string
				err = json.Unmarshal([]byte(output), &labels)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(labels["team"]).To(Equal("backend"))
				g.Expect(labels["env"]).To(Equal("production"))
			}, 1*time.Minute, time.Second).Should(Succeed())
		})

		It("should populate status.appliedLabels after successful reconciliation", func() {
			By("creating a NamespaceLabel CR")
			crYAML := fmt.Sprintf(`
apiVersion: namespacelabel.dana.io/v1alpha1
kind: NamespaceLabel
metadata:
  name: status-check
  namespace: %s
spec:
  labels:
    app: myapp
    version: v1
`, testNS)
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(crYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying status.appliedLabels contains the applied labels")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "namespacelabel", "status-check",
					"-n", testNS, "-o", "jsonpath={.status.appliedLabels}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				var appliedLabels []string
				err = json.Unmarshal([]byte(output), &appliedLabels)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(appliedLabels).To(ContainElements("app", "version"))
			}, 1*time.Minute, time.Second).Should(Succeed())
		})
	})

	Context("NamespaceLabel Updates", func() {
		const testNS = "e2e-update-test"

		BeforeEach(func() {
			By("creating a test namespace")
			cmd := exec.Command("kubectl", "create", "ns", testNS)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			By("cleaning up test namespace")
			cmd := exec.Command("kubectl", "delete", "ns", testNS, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should update namespace labels when NamespaceLabel spec is modified", func() {
			By("creating initial NamespaceLabel CR")
			crYAML := fmt.Sprintf(`
apiVersion: namespacelabel.dana.io/v1alpha1
kind: NamespaceLabel
metadata:
  name: update-test
  namespace: %s
spec:
  labels:
    color: blue
`, testNS)
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(crYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying initial label is applied")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "ns", testNS,
					"-o", "jsonpath={.metadata.labels.color}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("blue"))
			}, 1*time.Minute, time.Second).Should(Succeed())

			By("updating the NamespaceLabel CR with new value")
			crYAMLUpdate := fmt.Sprintf(`
apiVersion: namespacelabel.dana.io/v1alpha1
kind: NamespaceLabel
metadata:
  name: update-test
  namespace: %s
spec:
  labels:
    color: red
`, testNS)
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(crYAMLUpdate)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying label value is updated on namespace")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "ns", testNS,
					"-o", "jsonpath={.metadata.labels.color}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("red"))
			}, 1*time.Minute, time.Second).Should(Succeed())
		})

		It("should remove old labels when they are removed from spec", func() {
			By("creating NamespaceLabel with multiple labels")
			crYAML := fmt.Sprintf(`
apiVersion: namespacelabel.dana.io/v1alpha1
kind: NamespaceLabel
metadata:
  name: remove-test
  namespace: %s
spec:
  labels:
    keep-me: "yes"
    remove-me: "soon"
`, testNS)
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(crYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying both labels are applied")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "ns", testNS,
					"-o", "jsonpath={.metadata.labels}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				var labels map[string]string
				err = json.Unmarshal([]byte(output), &labels)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(labels["keep-me"]).To(Equal("yes"))
				g.Expect(labels["remove-me"]).To(Equal("soon"))
			}, 1*time.Minute, time.Second).Should(Succeed())

			By("updating CR to remove one label")
			crYAMLUpdate := fmt.Sprintf(`
apiVersion: namespacelabel.dana.io/v1alpha1
kind: NamespaceLabel
metadata:
  name: remove-test
  namespace: %s
spec:
  labels:
    keep-me: "yes"
`, testNS)
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(crYAMLUpdate)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying the removed label is cleaned up from namespace")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "ns", testNS,
					"-o", "jsonpath={.metadata.labels}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				var labels map[string]string
				err = json.Unmarshal([]byte(output), &labels)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(labels["keep-me"]).To(Equal("yes"))
				g.Expect(labels).NotTo(HaveKey("remove-me"))
			}, 1*time.Minute, time.Second).Should(Succeed())
		})
	})

	Context("NamespaceLabel Deletion", func() {
		const testNS = "e2e-deletion-test"

		BeforeEach(func() {
			By("creating a test namespace")
			cmd := exec.Command("kubectl", "create", "ns", testNS)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			By("cleaning up test namespace")
			cmd := exec.Command("kubectl", "delete", "ns", testNS, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should remove all managed labels when NamespaceLabel is deleted", func() {
			By("creating a NamespaceLabel CR")
			crYAML := fmt.Sprintf(`
apiVersion: namespacelabel.dana.io/v1alpha1
kind: NamespaceLabel
metadata:
  name: delete-me
  namespace: %s
spec:
  labels:
    managed-label-1: value1
    managed-label-2: value2
`, testNS)
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(crYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying labels are applied")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "ns", testNS,
					"-o", "jsonpath={.metadata.labels}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				var labels map[string]string
				err = json.Unmarshal([]byte(output), &labels)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(labels["managed-label-1"]).To(Equal("value1"))
				g.Expect(labels["managed-label-2"]).To(Equal("value2"))
			}, 1*time.Minute, time.Second).Should(Succeed())

			By("deleting the NamespaceLabel CR")
			cmd = exec.Command("kubectl", "delete", "namespacelabel", "delete-me", "-n", testNS)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying all managed labels are removed from namespace")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "ns", testNS,
					"-o", "jsonpath={.metadata.labels}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				var labels map[string]string
				err = json.Unmarshal([]byte(output), &labels)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(labels).NotTo(HaveKey("managed-label-1"))
				g.Expect(labels).NotTo(HaveKey("managed-label-2"))
			}, 1*time.Minute, time.Second).Should(Succeed())
		})
	})

	Context("Multiple NamespaceLabels in Same Namespace", func() {
		const testNS = "e2e-multi-test"

		BeforeEach(func() {
			By("creating a test namespace")
			cmd := exec.Command("kubectl", "create", "ns", testNS)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			By("cleaning up test namespace")
			cmd := exec.Command("kubectl", "delete", "ns", testNS, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should merge labels from multiple NamespaceLabel CRs", func() {
			By("creating first NamespaceLabel CR")
			crYAML1 := fmt.Sprintf(`
apiVersion: namespacelabel.dana.io/v1alpha1
kind: NamespaceLabel
metadata:
  name: first-cr
  namespace: %s
spec:
  labels:
    from-first: value1
`, testNS)
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(crYAML1)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("creating second NamespaceLabel CR")
			crYAML2 := fmt.Sprintf(`
apiVersion: namespacelabel.dana.io/v1alpha1
kind: NamespaceLabel
metadata:
  name: second-cr
  namespace: %s
spec:
  labels:
    from-second: value2
`, testNS)
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(crYAML2)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying labels from both CRs are present on namespace")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "ns", testNS,
					"-o", "jsonpath={.metadata.labels}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				var labels map[string]string
				err = json.Unmarshal([]byte(output), &labels)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(labels["from-first"]).To(Equal("value1"))
				g.Expect(labels["from-second"]).To(Equal("value2"))
			}, 1*time.Minute, time.Second).Should(Succeed())
		})

		It("should handle overlapping labels with deterministic precedence", func() {
			By("creating CR 'z-cr' with conflict label")
			crZ := fmt.Sprintf(`
apiVersion: namespacelabel.dana.io/v1alpha1
kind: NamespaceLabel
metadata:
  name: z-cr
  namespace: %s
spec:
  labels:
    conflict: value-from-z
    unique-z: only-z
`, testNS)
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(crZ)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("creating CR 'a-cr' with same conflict label")
			crA := fmt.Sprintf(`
apiVersion: namespacelabel.dana.io/v1alpha1
kind: NamespaceLabel
metadata:
  name: a-cr
  namespace: %s
spec:
  labels:
    conflict: value-from-a
    unique-a: only-a
`, testNS)
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(crA)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying merged labels - 'a-cr' should win conflict due to sort order")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "ns", testNS,
					"-o", "jsonpath={.metadata.labels}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				var labels map[string]string
				err = json.Unmarshal([]byte(output), &labels)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(labels["conflict"]).To(Equal("value-from-a"))
				g.Expect(labels["unique-z"]).To(Equal("only-z"))
				g.Expect(labels["unique-a"]).To(Equal("only-a"))
			}, 1*time.Minute, time.Second).Should(Succeed())

			By("deleting the winning CR 'a-cr'")
			cmd = exec.Command("kubectl", "delete", "namespacelabel", "a-cr", "-n", testNS)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying 'z-cr' value now takes precedence for conflict label")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "ns", testNS,
					"-o", "jsonpath={.metadata.labels}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				var labels map[string]string
				err = json.Unmarshal([]byte(output), &labels)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(labels["conflict"]).To(Equal("value-from-z"))
				g.Expect(labels).NotTo(HaveKey("unique-a"))
				g.Expect(labels["unique-z"]).To(Equal("only-z"))
			}, 1*time.Minute, time.Second).Should(Succeed())
		})
	})

	Context("Protected Labels", func() {
		const testNS = "e2e-protected-test"

		BeforeEach(func() {
			By("creating a test namespace")
			cmd := exec.Command("kubectl", "create", "ns", testNS)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			By("cleaning up test namespace")
			cmd := exec.Command("kubectl", "delete", "ns", testNS, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should not apply labels with protected prefixes", func() {
			By("creating a NamespaceLabel with a protected label")
			crYAML := fmt.Sprintf(`
apiVersion: namespacelabel.dana.io/v1alpha1
kind: NamespaceLabel
metadata:
  name: protected-test
  namespace: %s
spec:
  labels:
    allowed-label: allowed-value
    kubernetes.io/protected: should-not-apply
`, testNS)
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(crYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying only allowed label is present, protected label is not")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "ns", testNS,
					"-o", "jsonpath={.metadata.labels}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				var labels map[string]string
				err = json.Unmarshal([]byte(output), &labels)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(labels["allowed-label"]).To(Equal("allowed-value"))
				g.Expect(labels).NotTo(HaveKey("kubernetes.io/protected"))
			}, 1*time.Minute, time.Second).Should(Succeed())
		})

		It("should report protected labels in status.failedLabels", func() {
			By("creating a NamespaceLabel with protected labels")
			crYAML := fmt.Sprintf(`
apiVersion: namespacelabel.dana.io/v1alpha1
kind: NamespaceLabel
metadata:
  name: failed-status-test
  namespace: %s
spec:
  labels:
    good-label: works
    kubernetes.io/bad: blocked
    k8s.io/also-bad: also-blocked
`, testNS)
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(crYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying status.failedLabels contains protected labels")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "namespacelabel", "failed-status-test",
					"-n", testNS, "-o", "json")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())

				var cr struct {
					Status struct {
						AppliedLabels []string `json:"appliedLabels"`
						FailedLabels  []struct {
							Key    string `json:"key"`
							Reason string `json:"reason"`
						} `json:"failedLabels"`
					} `json:"status"`
				}
				err = json.Unmarshal([]byte(output), &cr)
				g.Expect(err).NotTo(HaveOccurred())

				g.Expect(cr.Status.AppliedLabels).To(ContainElement("good-label"))
				g.Expect(cr.Status.FailedLabels).To(HaveLen(2))

				failedKeys := make([]string, len(cr.Status.FailedLabels))
				for i, fl := range cr.Status.FailedLabels {
					failedKeys[i] = fl.Key
					g.Expect(fl.Reason).To(ContainSubstring("protected"))
				}
				g.Expect(failedKeys).To(ContainElements("kubernetes.io/bad", "k8s.io/also-bad"))
			}, 1*time.Minute, time.Second).Should(Succeed())
		})
	})

	Context("Pre-existing Namespace Labels", func() {
		const testNS = "e2e-preexisting-test"

		BeforeEach(func() {
			By("creating a test namespace with pre-existing labels")
			cmd := exec.Command("kubectl", "create", "ns", testNS)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			cmd = exec.Command("kubectl", "label", "ns", testNS, "pre-existing=original")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			By("cleaning up test namespace")
			cmd := exec.Command("kubectl", "delete", "ns", testNS, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should not remove pre-existing labels that are not managed", func() {
			By("creating a NamespaceLabel CR")
			crYAML := fmt.Sprintf(`
apiVersion: namespacelabel.dana.io/v1alpha1
kind: NamespaceLabel
metadata:
  name: preserve-test
  namespace: %s
spec:
  labels:
    new-managed: by-operator
`, testNS)
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(crYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying both pre-existing and new labels are present")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "ns", testNS,
					"-o", "jsonpath={.metadata.labels}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				var labels map[string]string
				err = json.Unmarshal([]byte(output), &labels)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(labels["pre-existing"]).To(Equal("original"))
				g.Expect(labels["new-managed"]).To(Equal("by-operator"))
			}, 1*time.Minute, time.Second).Should(Succeed())

			By("deleting the NamespaceLabel CR")
			cmd = exec.Command("kubectl", "delete", "namespacelabel", "preserve-test", "-n", testNS)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying pre-existing label is preserved after CR deletion")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "ns", testNS,
					"-o", "jsonpath={.metadata.labels}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				var labels map[string]string
				err = json.Unmarshal([]byte(output), &labels)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(labels["pre-existing"]).To(Equal("original"))
				g.Expect(labels).NotTo(HaveKey("new-managed"))
			}, 1*time.Minute, time.Second).Should(Succeed())
		})
	})
})
