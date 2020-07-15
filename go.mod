module knative.dev/net-istio

go 1.14

require (
	github.com/gobuffalo/envy v1.9.0 // indirect
	github.com/gogo/protobuf v1.3.1
	github.com/google/go-cmp v0.5.0
	go.uber.org/zap v1.14.1
	golang.org/x/sync v0.0.0-20200625203802-6e8e738ad208
	istio.io/api v0.0.0-20200601150056-da2b8f9da6d0
	istio.io/client-go v0.0.0-20200601150742-08b47b9edf56
	k8s.io/api v0.18.1
	k8s.io/apimachinery v0.18.5
	k8s.io/client-go v11.0.1-0.20190805182717-6502b5e7b1b5+incompatible
	knative.dev/networking v0.0.0-20200714175930-aa7079ce334c
	knative.dev/pkg v0.0.0-20200715175833-f50bf783a74d
	knative.dev/serving v0.16.1-0.20200715175733-5cec54ddaadf
	knative.dev/test-infra v0.0.0-20200715150133-fb8ce6a42ed4
)

replace (
	github.com/prometheus/client_golang => github.com/prometheus/client_golang v0.9.2
	k8s.io/api => k8s.io/api v0.17.6
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.17.6
	k8s.io/apimachinery => k8s.io/apimachinery v0.17.6
	k8s.io/client-go => k8s.io/client-go v0.17.6
	k8s.io/code-generator => k8s.io/code-generator v0.17.6
)
