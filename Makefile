.PHONY: bin test test-unit test-integration test-all coverage-html

bin:
	goreleaser build --snapshot --clean --single-target

# Default test target: unit tests only.
test: test-unit

# Unit tests — no integration build tag, no jj binary required.
# -coverpkg=./... so coverage of cross-package calls is counted (e.g.
# spr_test.go calls into vcs.JjOps, which we want reflected in vcs/jj_ops.go
# coverage).
test-unit:
	go test -race -coverpkg=./... -coverprofile=cov-unit.out -covermode=atomic ./...

# Integration tests — runs against a real `jj` binary. Set
# SPR_SKIP_JJ_INTEGRATION=1 to skip cleanly when jj is unavailable.
test-integration:
	go test -race -tags=integration -coverpkg=./... -coverprofile=cov-integration.out -covermode=atomic ./...

# Both suites, then merge profiles and print per-file attribution.
test-all: test-unit test-integration
	@go run ./scripts/coverage-merge cov-unit.out cov-integration.out cov-merged.out

# Generate HTML coverage reports for both profiles plus the merged view.
coverage-html: test-all
	go tool cover -html=cov-unit.out -o cov-unit.html
	go tool cover -html=cov-integration.out -o cov-integration.html
	go tool cover -html=cov-merged.out -o cov-merged.html
	@echo "Open cov-unit.html, cov-integration.html, cov-merged.html"
