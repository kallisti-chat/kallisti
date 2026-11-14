.PHONY: test typecheck lint check build

test:
	pnpm -r test
	cd server && go test -race ./...

typecheck:
	pnpm -r typecheck
	cd server && go vet ./...

lint:
	@test -z "$$(gofmt -l $$(git ls-files --cached --others --exclude-standard '*.go'))" || { echo 'run gofmt on Go sources'; exit 1; }

check: lint typecheck test

build:
	pnpm --filter @kallisti/web build
	cd server && go build -o /dev/null ./cmd/server
