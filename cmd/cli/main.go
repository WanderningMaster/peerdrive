package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	cmd "github.com/WanderningMaster/peerdrive/cmd/http"
	"github.com/WanderningMaster/peerdrive/internal/node"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	sub := os.Args[1]
	switch sub {
	case "serve-http":
		serveHTTP(os.Args[2:])
	case "serve":
		serve(os.Args[2:])
	case "put":
		putHTTP(os.Args[2:])
	case "get":
		getHTTP(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Println(`peerdrive <command> [flags]
Commands:
serve-http Run a node and start the built-in HTTP Client (/id, /put, /get)
serve Run a node (no HTTP)
put Use to store key/value on a running node
get Use to fetch value from a running node
Use -h after a command for flags.`)
}

func serveHTTP(args []string) {
	fs := flag.NewFlagSet("serve-http", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:9001", "TCP listen address (node)")
	boot := fs.String("bootstrap", "", "comma-separated peers to bootstrap (host:port)")
	fs.Parse(args)
	cmd.BootstrapHttpClient(addr, boot)
}

func serve(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:9001", "TCP listen address (node)")
	boot := fs.String("bootstrap", "", "comma-separated peers to bootstrap (host:port)")
	fs.Parse(args)

	n := node.NewNode(*addr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if *boot != "" {
		peers := splitCSV(*boot)
		go n.Bootstrap(ctx, peers)
	}

	// Graceful shutdown
	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
		<-ch
		cancel()
	}()

	if err := n.ListenAndServe(ctx); err != nil {
		log.Fatal(err)
	}
}

func putHTTP(args []string) {
	fs := flag.NewFlagSet("put", flag.ExitOnError)
	base := fs.String("http", "", "base URL of running node HTTP endpoint (e.g., http://127.0.0.1:8081)")
	key := fs.String("key", "", "key (string; passed as-is to server)")
	val := fs.String("value", "", "value")
	fs.Parse(args)
	if *base == "" {
		log.Fatal("-http is required (e.g., -http http://127.0.0.1:8081)")
	}
	if *key == "" {
		log.Fatal("-key is required")
	}

	u, err := url.Parse(*base)
	if err != nil {
		log.Fatal(err)
	}
	u.Path = "/put"
	q := u.Query()
	q.Set("key", *key)
	q.Set("value", *val)
	u.RawQuery = q.Encode()

	resp, err := http.Post(u.String(), "text/plain", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		log.Fatalf("server error: %s", strings.TrimSpace(string(b)))
	}
	fmt.Print(string(b))
}

func getHTTP(args []string) {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	base := fs.String("http", "", "base URL of running node HTTP endpoint (e.g., http://127.0.0.1:8082)")
	key := fs.String("key", "", "key (string; passed as-is to server)")
	fs.Parse(args)
	if *base == "" {
		log.Fatal("-http is required (e.g., -http http://127.0.0.1:8082)")
	}
	if *key == "" {
		log.Fatal("-key is required")
	}

	u, err := url.Parse(*base)
	if err != nil {
		log.Fatal(err)
	}
	u.Path = "/get"
	q := u.Query()
	q.Set("key", *key)
	u.RawQuery = q.Encode()

	resp, err := http.Get(u.String())
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		log.Fatalf("server error: %s", strings.TrimSpace(string(b)))
	}
	fmt.Print(string(b))
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
