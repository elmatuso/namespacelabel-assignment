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
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"namespacelabel.dana.io/test/utils"
)

const (
	namespace                  = "namespacelabel-assignment-system"
	defaultTestTimeout         = 30 * time.Second
	defaultTestPollingInterval = time.Second
)

// Helper functions for e2e tests

// createNamespaceLabel creates a NamespaceLabel CR with the given name, namespace, and labels
func createNamespaceLabel(name, targetNamespace string, labels map[string]string) error {
	labelYAML := generateNamespaceLabelYAML(name, targetNamespace, labels)
	return applyYAML(labelYAML)
}

// generateNamespaceLabelYAML generates YAML for a NamespaceLabel CR
func generateNamespaceLabelYAML(name, targetNamespace string, labels map[string]string) string {
	var labelLines strings.Builder
	for k, v := range labels {
		labelLines.WriteString(fmt.Sprintf("    %s: %s\n", k, v))
	}

	return fmt.Sprintf(`apiVersion: namespacelabel.dana.io/v1alpha1
kind: NamespaceLabel
metadata:
  name: %s
  namespace: %s
spec:
  labels:
%s`, name, targetNamespace, labelLines.String())
}

// applyYAML applies the given YAML string to the cluster
func applyYAML(yaml string) error {
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(yaml)
	_, err := utils.Run(cmd)
	return err
}

// deleteNamespaceLabel deletes a NamespaceLabel CR
func deleteNamespaceLabel(name, targetNamespace string) error {
	cmd := exec.Command("kubectl", "delete", "namespacelabel", name, "-n", targetNamespace)
	_, err := utils.Run(cmd)
	return err
}

// getNamespaceLabelValue retrieves the value of a specific label from a namespace
func getNamespaceLabelValue(namespaceName, labelKey string) (string, error) {
	cmd := exec.Command("kubectl", "get", "namespace", namespaceName,
		"-o", fmt.Sprintf("jsonpath={.metadata.labels.%s}", escapeJSONPath(labelKey)))
	return utils.Run(cmd)
}

// getAllNamespaceLabels retrieves all labels from a namespace as a string
func getAllNamespaceLabels(namespaceName string) (string, error) {
	cmd := exec.Command("kubectl", "get", "namespace", namespaceName, "-o", "jsonpath={.metadata.labels}")
	return utils.Run(cmd)
}

// getManagedLabelsAnnotation retrieves the managed-labels annotation from a namespace
func getManagedLabelsAnnotation(namespaceName string) (string, error) {
	cmd := exec.Command("kubectl", "get", "namespace", namespaceName,
		"-o", "jsonpath={.metadata.annotations.namespacelabel\\.dana\\.io/managed-labels}")
	return utils.Run(cmd)
}

// escapeJSONPath escapes special characters in label keys for JSONPath queries
func escapeJSONPath(key string) string {
	return strings.ReplaceAll(key, ".", "\\.")
}

// verifyNamespaceLabelExists verifies that a label with the expected value exists on the namespace
func verifyNamespaceLabelExists(namespaceName, labelKey, expectedValue string) {
	Eventually(func(g Gomega) {
		value, err := getNamespaceLabelValue(namespaceName, labelKey)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(value).To(Equal(expectedValue), fmt.Sprintf("Expected label %s=%s", labelKey, expectedValue))
	}, defaultTestTimeout, defaultTestPollingInterval).Should(Succeed())
}

// verifyNamespaceLabelNotExists verifies that a label does not exist on the namespace
func verifyNamespaceLabelNotExists(namespaceName, labelKey string) {
	Eventually(func(g Gomega) {
		value, err := getNamespaceLabelValue(namespaceName, labelKey)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(value).To(BeEmpty(), fmt.Sprintf("Label %s should not exist", labelKey))
	}, defaultTestTimeout, defaultTestPollingInterval).Should(Succeed())
}

// verifyNamespaceHasLabels verifies that the namespace contains all the specified label keys
func verifyNamespaceHasLabels(namespaceName string, labelKeys ...string) {
	Eventually(func(g Gomega) {
		allLabels, err := getAllNamespaceLabels(namespaceName)
		g.Expect(err).NotTo(HaveOccurred())
		for _, key := range labelKeys {
			g.Expect(allLabels).To(ContainSubstring(key), fmt.Sprintf("Expected label key %s", key))
		}
	}, defaultTestTimeout, defaultTestPollingInterval).Should(Succeed())
}

// verifyManagedLabelsAnnotation verifies that the managed-labels annotation contains the expected label keys
func verifyManagedLabelsAnnotation(namespaceName string, expectedKeys ...string) {
	Eventually(func(g Gomega) {
		annotation, err := getManagedLabelsAnnotation(namespaceName)
		g.Expect(err).NotTo(HaveOccurred())
		for _, key := range expectedKeys {
			g.Expect(annotation).To(ContainSubstring(key), fmt.Sprintf("Expected annotation to contain %s", key))
		}
	}, defaultTestTimeout, defaultTestPollingInterval).Should(Succeed())
}

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

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

	Context("NamespaceLabel Operator", func() {
		var testNamespace string

		BeforeEach(func() {
			testNamespace = fmt.Sprintf("test-ns-%d", time.Now().Unix())
			By(fmt.Sprintf("creating test namespace: %s", testNamespace))
			cmd := exec.Command("kubectl", "create", "ns", testNamespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create test namespace")
		})

		AfterEach(func() {
			By(fmt.Sprintf("deleting test namespace: %s", testNamespace))
			cmd := exec.Command("kubectl", "delete", "ns", testNamespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should apply labels from a NamespaceLabel CR to its namespace", func() {
			namespaceLabelName := "test-label-1"
			labels := map[string]string{
				"app":         "myapp",
				"environment": "production",
				"team":        "backend",
			}

			By("creating a NamespaceLabel with labels")
			err := createNamespaceLabel(namespaceLabelName, testNamespace, labels)
			Expect(err).NotTo(HaveOccurred(), "Failed to create NamespaceLabel")

			By("verifying that labels are applied to the namespace")
			verifyNamespaceLabelExists(testNamespace, "app", "myapp")
			verifyNamespaceLabelExists(testNamespace, "environment", "production")
			verifyNamespaceLabelExists(testNamespace, "team", "backend")

			By("verifying the managed-labels annotation is set")
			verifyManagedLabelsAnnotation(testNamespace, "app", "environment", "team")
		})

		It("should update labels when NamespaceLabel CR is modified", func() {
			namespaceLabelName := "test-label-2"

			By("creating a NamespaceLabel with initial labels")
			initialLabels := map[string]string{"version": "v1"}
			err := createNamespaceLabel(namespaceLabelName, testNamespace, initialLabels)
			Expect(err).NotTo(HaveOccurred())

			By("waiting for initial label to be applied")
			verifyNamespaceLabelExists(testNamespace, "version", "v1")

			By("updating the NamespaceLabel with new label value")
			updatedLabels := map[string]string{
				"version":   "v2",
				"new-label": "added",
			}
			err = createNamespaceLabel(namespaceLabelName, testNamespace, updatedLabels)
			Expect(err).NotTo(HaveOccurred())

			By("verifying the label value is updated")
			verifyNamespaceLabelExists(testNamespace, "version", "v2")

			By("verifying the new label is added")
			verifyNamespaceLabelExists(testNamespace, "new-label", "added")
		})

		It("should handle label conflicts using smallest-name-wins strategy", func() {
			By("creating two NamespaceLabels with conflicting labels")

			// nl-b should win because 'b' < 'z'
			labelsB := map[string]string{
				"conflict": "from-b",
				"unique-b": "value-b",
			}
			labelsZ := map[string]string{
				"conflict": "from-z",
				"unique-z": "value-z",
			}

			err := createNamespaceLabel("nl-z", testNamespace, labelsZ)
			Expect(err).NotTo(HaveOccurred())

			err = createNamespaceLabel("nl-b", testNamespace, labelsB)
			Expect(err).NotTo(HaveOccurred())

			By("verifying that smallest name wins the conflict")
			verifyNamespaceLabelExists(testNamespace, "conflict", "from-b")

			By("verifying that unique labels from both CRs are applied")
			verifyNamespaceHasLabels(testNamespace, "unique-b", "value-b", "unique-z", "value-z")
		})

		It("should remove labels when NamespaceLabel CR is deleted", func() {
			namespaceLabelName := "test-label-delete"
			labels := map[string]string{
				"temporary": "label",
				"remove-me": "value",
			}

			By("creating a NamespaceLabel with labels")
			err := createNamespaceLabel(namespaceLabelName, testNamespace, labels)
			Expect(err).NotTo(HaveOccurred())

			By("waiting for labels to be applied")
			verifyNamespaceLabelExists(testNamespace, "temporary", "label")

			By("deleting the NamespaceLabel CR")
			err = deleteNamespaceLabel(namespaceLabelName, testNamespace)
			Expect(err).NotTo(HaveOccurred())

			By("verifying that managed labels are removed from the namespace")
			verifyNamespaceLabelNotExists(testNamespace, "temporary")
			verifyNamespaceLabelNotExists(testNamespace, "remove-me")
		})

		It("should not modify protected kubernetes.io/* labels", func() {
			namespaceLabelName := "test-protected"

			By("getting the original kubernetes.io/metadata.name label")
			originalValue, err := getNamespaceLabelValue(testNamespace, "kubernetes.io/metadata.name")
			Expect(err).NotTo(HaveOccurred())
			Expect(originalValue).To(Equal(testNamespace))

			By("attempting to create a NamespaceLabel that tries to modify a protected label")
			labels := map[string]string{
				"kubernetes.io/metadata.name": "hacked",
				"safe-label":                  "allowed",
			}
			err = createNamespaceLabel(namespaceLabelName, testNamespace, labels)
			Expect(err).NotTo(HaveOccurred())

			By("waiting for the operator to process the CR")
			time.Sleep(5 * time.Second)

			By("verifying that the protected label was NOT modified")
			currentValue, err := getNamespaceLabelValue(testNamespace, "kubernetes.io/metadata.name")
			Expect(err).NotTo(HaveOccurred())
			Expect(currentValue).To(Equal(originalValue), "Protected label should not be modified")

			By("verifying that non-protected labels were applied")
			verifyNamespaceLabelExists(testNamespace, "safe-label", "allowed")
		})
	})
})
