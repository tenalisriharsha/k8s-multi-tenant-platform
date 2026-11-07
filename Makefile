BINARY := tenantctl

.PHONY: build test vet fmt clean

build:
	go build -o bin/$(BINARY) ./cmd/tenantctl

test:
	go test ./... -cover

vet:
	go vet ./...

fmt:
	gofmt -l .

clean:
	rm -rf bin
