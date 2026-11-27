.PHONY: check test vet lint build
check: lint vet test
lint:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }
vet:
	go vet ./...
test:
	go test -race ./...
build:
	go build -trimpath -o bin/preview-controller ./cmd/controller
.PHONY: workflow-check
workflow-check:
	python3 tests/workflows_test.py

.PHONY: smoke
smoke:
	python3 tests/smoke.py
