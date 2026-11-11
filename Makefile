BINARY := tenantctl

.PHONY: build test envtest vet fmt clean

build:
	go build -o bin/$(BINARY) ./cmd/tenantctl

test:
	go test ./... -cover

# Downloads kubebuilder's etcd/kube-apiserver test binaries (if not already
# cached) and runs the envtest-tagged suite against a real, ephemeral API
# server. Needs no Docker or kind cluster, unlike `go test -tags e2e`.
envtest:
	KUBEBUILDER_ASSETS="$$(go run sigs.k8s.io/controller-runtime/tools/setup-envtest@v0.25.2 use 1.33.0 -p path)" \
		go test -tags envtest ./... -cover

vet:
	go vet ./...

fmt:
	gofmt -l .

clean:
	rm -rf bin
