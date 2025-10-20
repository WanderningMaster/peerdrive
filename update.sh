git pull
systemctl --user stop peerdrived.service
make build && sudo cp ./bin/peerdrive /usr/local/bin/peerdrive
systemctl --user start peerdrived.service
