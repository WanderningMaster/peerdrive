# Multi-stage build for the PeerDrive CLI container

FROM golang:1.24.5-alpine AS builder
WORKDIR /src

# Cache deps first
COPY go.mod go.sum ./
RUN go mod download

# Build CLI
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /peerdrive ./cmd/cli

# Provide a tiny shell for debugging/exec
FROM busybox:1.36.1-musl AS busybox

# Minimal runtime image
FROM scratch
COPY --from=builder /peerdrive /peerdrive
COPY --from=busybox /bin/busybox /bin/sh

# Persist config and blockstore between runs if mounted by the user
VOLUME ["/root/.config/peerdrive", "/root/.peerdrive"]

# Forward all args to `peerdrive init`
ENTRYPOINT ["/peerdrive", "init"]

# Users can pass flags like:
# docker run --rm <image> --bootstrap host1:30000,host2:30001 --relay host:35000 --mem
