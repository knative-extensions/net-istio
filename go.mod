module knative.dev/net-istio

go 1.16

require (
	github.com/gogo/protobuf v1.3.2
	github.com/google/go-cmp v0.5.6
	go.uber.org/zap v1.19.1
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	istio.io/api v0.0.0-20210713195055-3a340e4f154e
	istio.io/client-go v1.10.3
	k8s.io/api v0.23.5
	k8s.io/apimachinery v0.23.5
	k8s.io/client-go v0.23.5
	knative.dev/hack v0.0.0-20220411131823-6ffd8417de7c
	knative.dev/networking v0.0.0-20220426062255-9f491cb067cd
	knative.dev/pkg v0.0.0-20220425215754-45c37c266b74
)
