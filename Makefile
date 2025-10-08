build:
	go build -o bin/peerdrive cmd/cli/main.go
build-simulation:
	go build -o bin/simulation cmd/simulation/main.go
test:
	go test ./... -v

%:
	@true
