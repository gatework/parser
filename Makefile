.PHONY: check fuzz

check:
	GOWORK=off go test -race ./...
	GOWORK=off go vet ./...

fuzz:
	GOWORK=off go test -run='^$$' -fuzz='^FuzzContent$$' -fuzztime=10s -parallel=4 .
	GOWORK=off go test -run='^$$' -fuzz='^FuzzCursor$$' -fuzztime=10s -parallel=4 .
