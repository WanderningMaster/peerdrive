build:
	go build -o bin/peerdrive cmd/cli/*
build-localnet:
	go build -o bin/localnet cmd/localnet/main.go
build-dfs-demo:
	go build -o bin/dfs-demo cmd/dfs-demo/main.go
test:
	go test ./... -v
daemonize:
	make build
	sudo cp ./bin/peerdrive /usr/local/bin/peerdrive
	cp peerdrived.service $$HOME/.config/systemd/user/peerdrived.service
	sudo chown $$(whoami) $$HOME/.config/systemd/user/peerdrived.service

%:
	@true
