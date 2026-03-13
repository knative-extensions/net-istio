/*
Copyright 2026 The Knative Authors

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

package resources

import (
	"encoding/json"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
	apisnet "knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/networking/pkg/http/header"
	"knative.dev/networking/pkg/ingress"
	"knative.dev/pkg/network"
)

// TagToHosts parses the tag-to-host annotation into sets keyed by tag.
// Invalid annotations and empty host lists are ignored.
func TagToHosts(ing *v1alpha1.Ingress) map[string]sets.Set[string] {
	serialized := ing.GetAnnotations()[apisnet.TagToHostAnnotationKey]
	if serialized == "" {
		return nil
	}

	parsed := make(map[string][]string)
	if err := json.Unmarshal([]byte(serialized), &parsed); err != nil {
		return nil
	}

	tagToHosts := make(map[string]sets.Set[string], len(parsed))
	for tag, hosts := range parsed {
		if len(hosts) == 0 {
			continue
		}
		tagToHosts[tag] = sets.New(hosts...)
	}
	return tagToHosts
}

// HostsForTag returns the hostnames for a tag filtered by ingress visibility.
func HostsForTag(tag string, visibility v1alpha1.IngressVisibility, tagToHosts map[string]sets.Set[string]) sets.Set[string] {
	if len(tagToHosts) == 0 {
		return sets.New[string]()
	}
	hosts, ok := tagToHosts[tag]
	if !ok {
		return sets.New[string]()
	}

	switch visibility {
	case v1alpha1.IngressVisibilityClusterLocal:
		return ingress.ExpandedHosts(filterLocalHostnames(hosts))
	default:
		return ingress.ExpandedHosts(filterNonLocalHostnames(hosts))
	}
}

// MakeTagHostIngressPath clones a header-based tag path into a host-based one.
func MakeTagHostIngressPath(path *v1alpha1.HTTPIngressPath, tag string) *v1alpha1.HTTPIngressPath {
	tagPath := path.DeepCopy()
	if tagPath.Headers != nil {
		delete(tagPath.Headers, header.RouteTagKey)
		if len(tagPath.Headers) == 0 {
			tagPath.Headers = nil
		}
	}
	if tagPath.AppendHeaders == nil {
		tagPath.AppendHeaders = make(map[string]string, 1)
	}
	tagPath.AppendHeaders[header.RouteTagKey] = tag
	return tagPath
}

// RouteTagHeaderValue returns the value of the route tag header match.
func RouteTagHeaderValue(headers map[string]v1alpha1.HeaderMatch) string {
	if len(headers) == 0 {
		return ""
	}
	match, ok := headers[header.RouteTagKey]
	if !ok {
		return ""
	}
	return match.Exact
}

// RouteTagAppendValue returns the value of the route tag append header.
func RouteTagAppendValue(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}
	return headers[header.RouteTagKey]
}

// RouteHosts returns the hostnames addressed by a path, including synthesized
// tag hosts for append-header-only tag routes.
func RouteHosts(ruleHosts sets.Set[string], path *v1alpha1.HTTPIngressPath, visibility v1alpha1.IngressVisibility, tagToHosts map[string]sets.Set[string]) sets.Set[string] {
	hosts := ruleHosts
	if tag := RouteTagAppendValue(path.AppendHeaders); tag != "" && RouteTagHeaderValue(path.Headers) == "" {
		hosts = hosts.Union(HostsForTag(tag, visibility, tagToHosts))
	}
	return hosts
}

func filterLocalHostnames(hosts sets.Set[string]) sets.Set[string] {
	return keepLocalHostnames(hosts)
}

func filterNonLocalHostnames(hosts sets.Set[string]) sets.Set[string] {
	localSvcSuffix := ".svc." + network.GetClusterDomainName()
	retained := sets.New[string]()
	for _, host := range sets.List(hosts) {
		if !strings.HasSuffix(host, localSvcSuffix) {
			retained.Insert(host)
		}
	}
	return retained
}
