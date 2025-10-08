build:
	go build -o bin/peerdrive cmd/cli/main.go
test:
	go test ./... -v

%:
	@true
