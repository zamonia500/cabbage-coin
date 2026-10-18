HOST ?= cabbage.zamonia500.com

.PHONY: test build deploy

test:
	go vet ./... && go test ./...

# Oracle Ampere A1 is linux/arm64; pure Go, so no cgo toolchain is needed.
build:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o cabbage .

deploy: test build
	scp cabbage $(HOST):/tmp/cabbage
	ssh $(HOST) 'sudo install -m 755 /tmp/cabbage /opt/cabbage/cabbage && sudo systemctl restart cabbage'
