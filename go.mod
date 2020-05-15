module knative.dev/net-istio

go 1.13

require (
	github.com/gobuffalo/envy v1.9.0 // indirect
	github.com/gogo/protobuf v1.3.1
	github.com/google/go-cmp v0.4.0
	github.com/gorilla/websocket v1.4.1 // indirect
	github.com/rogpeppe/go-internal v1.5.2 // indirect
	go.uber.org/zap v1.14.1
	golang.org/x/sync v0.0.0-20200317015054-43a5402ce75a
	gomodules.xyz/jsonpatch/v2 v2.1.0 // indirect
	istio.io/api v0.0.0-20200107183329-ed4b507c54e1
	istio.io/client-go v0.0.0-20200107185429-9053b0f86b03
	k8s.io/api v0.17.4
	k8s.io/apimachinery v0.17.4
	k8s.io/client-go v11.0.1-0.20190805182717-6502b5e7b1b5+incompatible
	knative.dev/pkg v0.0.0-20200515175802-f6cfa4c47cda
	knative.dev/serving v0.14.1-0.20200515175809-3574a9ae856c
	knative.dev/test-infra v0.0.0-20200515184601-7a28f47cdbcb
)

replace (
	github.com/prometheus/client_golang => github.com/prometheus/client_golang v0.9.2
	k8s.io/api => k8s.io/api v0.16.4
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.16.4
	k8s.io/apimachinery => k8s.io/apimachinery v0.16.4
	k8s.io/client-go => k8s.io/client-go v0.16.4
	k8s.io/code-generator => k8s.io/code-generator v0.16.4
)
