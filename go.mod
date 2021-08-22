module knative.dev/net-istio

go 1.16

require (
	github.com/gogo/protobuf v1.3.2
	github.com/google/go-cmp v0.5.6
	go.uber.org/zap v1.18.1
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	istio.io/api v0.0.0-20210322145030-ec7ef4cd6eaf
	istio.io/client-go v1.8.1
	k8s.io/api v0.20.7
	k8s.io/apimachinery v0.20.7
	k8s.io/client-go v0.20.7
	knative.dev/hack v0.0.0-20210806075220-815cd312d65c
	knative.dev/networking v0.0.0-20210820164705-3e603fb4df8a
	knative.dev/pkg v0.0.0-20210820164105-52d2a0db1e21
)
