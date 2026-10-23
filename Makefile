.PHONY: test vet exact experiment bench

test:
	go test -race -cover ./...

vet:
	go vet ./...

exact:
	go run ./cmd/relaylab exact

experiment:
	go run ./cmd/relaylab experiment -seed 42 -runs 30 -trials 10000 -out results/paper-42

bench:
	go test ./internal/selector -bench . -benchmem
