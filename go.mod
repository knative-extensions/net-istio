module knative.dev/net-istio

go 1.15

require (
	github.com/gogo/protobuf v1.3.2
	github.com/google/go-cmp v0.5.6
	go.uber.org/zap v1.17.0
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	istio.io/api v0.0.0-20210322145030-ec7ef4cd6eaf
	istio.io/client-go v1.8.1
	k8s.io/api v0.20.7
	k8s.io/apimachinery v0.20.7
	k8s.io/client-go v0.20.7
	knative.dev/hack v0.0.0-20210610231243-3d4b264d9472
	knative.dev/networking v0.0.0-20210611092143-1be893df2498
	knative.dev/pkg v0.0.0-20210611082343-dd15eb09982b
)
