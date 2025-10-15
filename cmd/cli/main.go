package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	cmd "github.com/WanderningMaster/peerdrive/cmd/http"
	"github.com/WanderningMaster/peerdrive/configuration"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	sub := os.Args[1]
	switch sub {
	case "init":
		serveHTTP(os.Args[2:])
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
init Run a node and start the built-in HTTP Client (/id, /put, /get)
put Use to store key/value on a running node
get Use to fetch value from a running node
Use -h after a command for flags.`)
}

func serveHTTP(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	boot := fs.String("bootstrap", "", "comma-separated peers to bootstrap (host:port)")
	fs.Parse(args)

	conf := configuration.LoadUserConfig()

	cmd.BootstrapHttpClient(conf, boot)

}

func putHTTP(args []string) {
	conf := configuration.LoadUserConfig()

	fs := flag.NewFlagSet("put", flag.ExitOnError)
	key := fs.String("key", "", "key (string; passed as-is to server)")
	val := fs.String("value", "", "value")
	fs.Parse(args)
	if *key == "" {
		log.Fatal("-key is required")
	}

	u, err := url.Parse(fmt.Sprintf("http://0.0.0.0:%d", conf.HttpPort))
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
	conf := configuration.LoadUserConfig()

	fs := flag.NewFlagSet("get", flag.ExitOnError)
	key := fs.String("key", "", "key (string; passed as-is to server)")
	fs.Parse(args)
	if *key == "" {
		log.Fatal("-key is required")
	}

	u, err := url.Parse(fmt.Sprintf("http://0.0.0.0:%d", conf.HttpPort))
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
