# Routing Decision External Processing Server

This is an [external processing](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_proc_filter) server that aims to process routing decisions based on a header `preferred-svc` or based on a response from an external service. For a example service take a look at [this project](https://github.com/day0ops/randomise-route-keys).

It is expected for the external service to respond with the body format,

```json
{
  "decision": "<some value>"
}
```

It will send a response to Envoy with the header `x-routing-decision` and remove any router cache. The receiving Envoy proxy can perform the decision based on this incoming header. If no header is present it will continue the request as normal.

## Build

- Use `make build` to build this service.
- To build and push the Docker images use `PUSH_MULTIARCH=true make docker`. By default, it only builds `linux/amd64` & `linux/arm64`.
  - If podman is available it will use `podman build` otherwise will fallback to `docker buildx`.
  - The images get pushed to `australia-southeast1-docker.pkg.dev/solo-test-236622/apac` but you can override this with the env var `REPO`.
- Run make help for all the build directives.

## Test

To run the test suite use `make test`.

The e2e test suite uses [testcontainers](https://golang.testcontainers.org/) hence requires Docker.

If using `podman` instead of Docker then make sure the socket is set correctly by following,

```bash
export TESTCONTAINERS_RYUK_DISABLED=true
export DOCKER_HOST=unix://$XDG_RUNTIME_DIR/podman/podman.sock
systemctl --user restart podman.socket
```