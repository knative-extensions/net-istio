module knative.dev/net-istio

go 1.15

require (
	github.com/gogo/protobuf v1.3.2
	github.com/google/go-cmp v0.5.5
	go.uber.org/zap v1.16.0
	golang.org/x/sync v0.0.0-20201207232520-09787c993a3a
	istio.io/api v0.0.0-20201123152548-197f11e4ea09
	istio.io/client-go v1.8.1
	istio.io/gogo-genproto v0.0.0-20191029161641-f7d19ec0141d // indirect
	k8s.io/api v0.19.7
	k8s.io/apimachinery v0.19.7
	k8s.io/client-go v0.19.7
	knative.dev/hack v0.0.0-20210309141825-9b73a256fd9a
	knative.dev/networking v0.0.0-20210311190926-430a689490e6
	knative.dev/pkg v0.0.0-20210315160101-6a33a1ab29ac
)
