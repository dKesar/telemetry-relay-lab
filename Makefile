.PHONY: test vet exact experiment bench demo timeline offline research

research:
	go run ./cmd/relaylab research -runs 30 -seed 1000 -out results/research-v3

demo:
	go run ./cmd/relaylab serve

timeline:
	go run ./cmd/relaylab timeline -runs 30 -seed 42 -out results/timeline-42

offline:
	go run ./cmd/relaylab offline -suite -out results/demo.html

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
