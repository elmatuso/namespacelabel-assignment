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

import "strings"

// protectedPrefixes contains a list of label prefixes that are protected and
// should not be modified or deleted by the operator.
var protectedPrefixes = []string{"kubernetes.io/", "k8s.io/"}

// isProtected checks if a label key is protected (starts with system prefixes).
func isProtected(key string) bool {
	for _, p := range protectedPrefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}
