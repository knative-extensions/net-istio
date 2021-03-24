module knative.dev/net-istio

go 1.15

require (
	github.com/gogo/protobuf v1.3.2
	github.com/google/go-cmp v0.5.4
	go.uber.org/zap v1.16.0
	golang.org/x/sync v0.0.0-20201207232520-09787c993a3a
	istio.io/api v0.0.0-20210322145030-ec7ef4cd6eaf
	istio.io/client-go v1.8.1
<<<<<<< HEAD
	istio.io/gogo-genproto v0.0.0-20191029161641-f7d19ec0141d // indirect
	k8s.io/api v0.19.7
	k8s.io/apimachinery v0.19.7
	k8s.io/client-go v0.19.7
	knative.dev/hack v0.0.0-20210203173706-8368e1f6eacf
	knative.dev/networking v0.0.0-20210216014426-94bfc013982b
	knative.dev/pkg v0.0.0-20210216013737-584933f8280b
=======
	k8s.io/api v0.20.5
	k8s.io/apimachinery v0.20.5
	k8s.io/client-go v0.20.5
	knative.dev/hack v0.0.0-20210317214554-58edbdc42966
	knative.dev/networking v0.0.0-20210323181619-8fc278deb519
	knative.dev/pkg v0.0.0-20210323202917-b558677ab034
>>>>>>> 36785d48 (Update net-istio with istio canonical label changes)
)
