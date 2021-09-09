module knative.dev/net-istio

go 1.16

require (
	github.com/gogo/protobuf v1.3.2
	github.com/google/go-cmp v0.5.6
	go.uber.org/zap v1.19.0
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	istio.io/api v0.0.0-20210322145030-ec7ef4cd6eaf
	istio.io/client-go v1.8.1
	k8s.io/api v0.21.4
	k8s.io/apimachinery v0.21.4
	k8s.io/client-go v0.21.4
	knative.dev/hack v0.0.0-20210806075220-815cd312d65c
	knative.dev/networking v0.0.0-20210909132459-78c491e7b7f0
	knative.dev/pkg v0.0.0-20210909102158-d569db39a812
)
