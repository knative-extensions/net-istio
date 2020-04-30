module knative.dev/net-istio

go 1.13

require (
	github.com/gobuffalo/envy v1.9.0 // indirect
	github.com/gogo/protobuf v1.3.1
	github.com/google/go-cmp v0.4.0
	github.com/google/licenseclassifier v0.0.0-20190926221455-842c0d70d702 // indirect
	github.com/gorilla/websocket v1.4.1 // indirect
	github.com/rogpeppe/go-internal v1.5.2 // indirect
	go.uber.org/zap v1.10.0
	golang.org/x/sync v0.0.0-20200317015054-43a5402ce75a
	gomodules.xyz/jsonpatch/v2 v2.1.0 // indirect
	istio.io/api v0.0.0-20200107183329-ed4b507c54e1
	istio.io/client-go v0.0.0-20200107185429-9053b0f86b03
	k8s.io/api v0.17.4
	k8s.io/apiextensions-apiserver v0.17.2 // indirect
	k8s.io/apimachinery v0.17.4
	k8s.io/client-go v11.0.1-0.20190805182717-6502b5e7b1b5+incompatible
	knative.dev/pkg v0.0.0-20200430190142-3d369cddd573
	knative.dev/serving v0.14.1-0.20200430203042-e46b7bc4f390
	knative.dev/test-infra v0.0.0-20200429211942-f4c4853375cf
)

replace (
	github.com/prometheus/client_golang => github.com/prometheus/client_golang v0.9.2
	k8s.io/api => k8s.io/api v0.16.4
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.16.4
	k8s.io/apimachinery => k8s.io/apimachinery v0.16.4
	k8s.io/client-go => k8s.io/client-go v0.16.4
	k8s.io/code-generator => k8s.io/code-generator v0.16.4
)
