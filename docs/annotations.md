# Annotations

You can add these Kubernetes annotations to specific `ksvc` or `Route` objects to customize their behavior.

Annotation keys and values can only be strings. Other types, such as boolean or numeric values must be quoted,i.e. `"true"`, `"false"`, `"100"`.

The annotation prefix must be `istio.ingress.networking.knative.dev`

### Enable CORS

To enable Cross-Origin Resource Sharing (CORS) in an KIngress rule, add the annotation
`istio.ingress.networking.knative.dev/enable-cors: "true"` to `ksvc` or `Route`. This will add a section in the server
location enabling this functionality.


CORS can be controlled with the following annotations:

* `istio.ingress.networking.knative.dev/cors-allow-methods`
  controls which methods are accepted. This is a multi-valued field, separated by ',' and
  accepts only letters (upper and lower case).
  - Default: `GET,PUT,POST,DELETE,PATCH,OPTIONS`
  - Example: `istio.ingress.networking.knative.dev/cors-allow-methods: "PUT, GET, POST, OPTIONS"`

* `istio.ingress.networking.knative.dev/cors-allow-headers`
  controls which headers are accepted. This is a multi-valued field, separated by ',' and accepts letters,
  numbers, _ and -.
  - Default: `DNT,X-CustomHeader,Keep-Alive,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type,Authorization`
  - Example: `istio.ingress.networking.knative.dev/cors-allow-headers: "X-Forwarded-For, X-app123-XPTO"`

* `istio.ingress.networking.knative.dev/cors-expose-headers`
  controls which headers are exposed to response.This is a multi-valued field, separated by ',' and
  accepts only letters (upper and lower case).
  - Default: *empty*
  - Example: `istio.ingress.networking.knative.dev/cors-expose-headers: "*, X-CustomResponseHeader"`

* `istio.ingress.networking.knative.dev/cors-allow-origin`
  controls what's the accepted Origin for CORS.
  This is a single field value, with the following format: `http(s)://origin-site.com` or `http(s)://origin-site.com:port`
  - Default: `*`
  - Example: `istio.ingress.networking.knative.dev/cors-allow-origin: "https://origin-site.com:4443"`

* `istio.ingress.networking.knative.dev/cors-allow-credentials`
  controls if credentials can be passed during CORS operations.
  - Default: `true`
  - Example: `istio.ingress.networking.knative.dev/cors-allow-credentials: "false"`

* `istio.ingress.networking.knative.dev/cors-max-age`
  controls how long preflight requests can be cached.
  Default: `1728000`
  Example: `istio.ingress.networking.knative.dev/cors-max-age: 600`
