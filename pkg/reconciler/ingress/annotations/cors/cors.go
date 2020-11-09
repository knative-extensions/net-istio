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

package cors

import (
	"regexp"

	"knative.dev/net-istio/pkg/reconciler/ingress/annotations/parser"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
)

// DefaultAnnotationsPrefix defines the common prefix used in the nginx ingress controller
const DefaultAnnotationsPrefix = "istio.ingress.networking.knative.dev"

const (
	// Default values
	DefaultCorsMethods = "GET,PUT,POST,DELETE,PATCH,OPTIONS"
	DefaultCorsHeaders = "DNT,X-CustomHeader,Keep-Alive,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type,Authorization"
	DefaultCorsMaxAge  = 1728000
)

var (
	// Regex are defined here to prevent information leak, if user tries to set anything not valid
	// that could cause the Response to contain some internal value/variable (like returning $pid, $upstream_addr, etc)
	// Origin must contain a http/s Origin (including or not the port) or the value '*'
	corsOriginRegex = regexp.MustCompile(`^(https?://[A-Za-z0-9\-\.]*(:[0-9]+)?|\*)?$`)
	// Method must contain valid methods list (PUT, GET, POST, BLA)
	// May contain or not spaces between each verb
	corsMethodsRegex = regexp.MustCompile(`^([A-Za-z]+,?\s?)+$`)
	// Headers must contain valid values only (X-HEADER12, X-ABC)
	// May contain or not spaces between each Header
	corsHeadersRegex = regexp.MustCompile(`^([A-Za-z0-9\-\_]+,?\s?)+$`)
	// Expose Headers must contain valid values only (*, X-HEADER12, X-ABC)
	// May contain or not spaces between each Header
	corsExposeHeadersRegex = regexp.MustCompile(`^(([A-Za-z0-9\-\_]+|\*),?\s?)+$`)
)

// Config contains the Cors configuration to be used in the Ingress
type Config struct {
	CorsEnabled          bool   `json:"corsEnabled"`
	CorsAllowOrigins     string `json:"corsAllowOrigins"`
	CorsAllowMethods     string `json:"corsAllowMethods"`
	CorsAllowHeaders     string `json:"corsAllowHeaders"`
	CorsAllowCredentials bool   `json:"corsAllowCredentials"`
	CorsExposeHeaders    string `json:"corsExposeHeaders"`
	CorsMaxAge           int    `json:"corsMaxAge"`
}

// Parse parses the annotations contained in the Kingress
// rule used to indicate if the location/s should allows CORS
func Parse(ing *v1alpha1.Ingress) *Config {
	var err error
	config := &Config{}
	annotations := ing.GetAnnotations()
	if len(annotations) == 0 {
		return config
	}

	config.CorsEnabled, err = parser.GetBoolAnnotation("enable-cors", ing)
	if err != nil {
		config.CorsEnabled = false
	}

	config.CorsAllowOrigins, err = parser.GetStringAnnotation("cors-allow-origin", ing)
	if err != nil || !corsOriginRegex.MatchString(config.CorsAllowOrigins) {
		config.CorsAllowOrigins = "*"
	}

	config.CorsAllowHeaders, err = parser.GetStringAnnotation("cors-allow-headers", ing)
	if err != nil || !corsHeadersRegex.MatchString(config.CorsAllowHeaders) {
		config.CorsAllowHeaders = DefaultCorsHeaders
	}

	config.CorsAllowMethods, err = parser.GetStringAnnotation("cors-allow-methods", ing)
	if err != nil || !corsMethodsRegex.MatchString(config.CorsAllowMethods) {
		config.CorsAllowMethods = DefaultCorsMethods
	}

	config.CorsAllowCredentials, err = parser.GetBoolAnnotation("cors-allow-credentials", ing)
	if err != nil {
		config.CorsAllowCredentials = true
	}

	config.CorsExposeHeaders, err = parser.GetStringAnnotation("cors-expose-headers", ing)
	if err != nil || !corsExposeHeadersRegex.MatchString(config.CorsExposeHeaders) {
		config.CorsExposeHeaders = ""
	}

	config.CorsMaxAge, err = parser.GetIntAnnotation("cors-max-age", ing)
	if err != nil {
		config.CorsMaxAge = DefaultCorsMaxAge
	}

	return config
}
