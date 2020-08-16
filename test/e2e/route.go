/*
Copyright 2020 The Knative Authors

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
	"knative.dev/pkg/test/spoof"
)

// RetryingRouteInconsistency retries common requests seen when creating a new route
func RetryingRouteInconsistency(innerCheck spoof.ResponseChecker) spoof.ResponseChecker {
	return func(resp *spoof.Response) (bool, error) {
		// If we didn't match any retryable codes, invoke the ResponseChecker that we wrapped.
		return innerCheck(resp)
	}
}
