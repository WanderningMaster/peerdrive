package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
	case "dfs-put":
		dfsPutHTTP(os.Args[2:])
	case "dfs-get":
		dfsGetHTTP(os.Args[2:])
	case "bootstrap":
		bootstrapHTTP(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Println(`peerdrive <command> [flags]
Commands:
init Run a node and start the built-in HTTP Client (/id, /put, /get, /dfs/{cid}, /dfs/put)
put Use to store key/value on a running node
get Use to fetch value from a running node
dfs-put Add file to DFS from path; prints resulting CID
dfs-get Fetch DFS content by CID and write to path
bootstrap Connect a running node to comma-separated peers
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

func dfsPutHTTP(args []string) {
	conf := configuration.LoadUserConfig()

	fs := flag.NewFlagSet("dfs-put", flag.ExitOnError)
	in := fs.String("in", "", "input file path to add to DFS")
	fs.Parse(args)
	if *in == "" {
		log.Fatal("-in is required")
	}
	inPath := *in
	if !filepath.IsAbs(inPath) {
		cwd, err := os.Getwd()
		if err != nil {
			log.Fatal(err)
		}
		inPath = filepath.Clean(filepath.Join(cwd, inPath))
	}

	u, err := url.Parse(fmt.Sprintf("http://0.0.0.0:%d", conf.HttpPort))
	if err != nil {
		log.Fatal(err)
	}
	u.Path = "/dfs/put"
	q := u.Query()
	q.Set("in", inPath)
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

func dfsGetHTTP(args []string) {
	conf := configuration.LoadUserConfig()

	fs := flag.NewFlagSet("dfs-get", flag.ExitOnError)
	cid := fs.String("cid", "", "CID of the DFS manifest to fetch")
	out := fs.String("out", "", "output file path to write the fetched content")
	fs.Parse(args)
	if *cid == "" {
		log.Fatal("-cid is required")
	}
	if *out == "" {
		log.Fatal("-out is required")
	}

	outPath := *out
	if !filepath.IsAbs(outPath) {
		cwd, err := os.Getwd()
		if err != nil {
			log.Fatal(err)
		}
		outPath = filepath.Clean(filepath.Join(cwd, outPath))
	}

	u, err := url.Parse(fmt.Sprintf("http://0.0.0.0:%d", conf.HttpPort))
	if err != nil {
		log.Fatal(err)
	}
	u.Path = "/dfs/" + *cid
	q := u.Query()
	q.Set("out", outPath)
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

func bootstrapHTTP(args []string) {
	conf := configuration.LoadUserConfig()

	fs := flag.NewFlagSet("bootstrap", flag.ExitOnError)
	peers := fs.String("peers", "", "comma-separated peers to bootstrap (host:port)")
	fs.Parse(args)
	if *peers == "" {
		log.Fatal("-peers is required")
	}

	u, err := url.Parse(fmt.Sprintf("http://0.0.0.0:%d", conf.HttpPort))
	if err != nil {
		log.Fatal(err)
	}
	u.Path = "/bootstrap"
	q := u.Query()
	q.Set("peers", *peers)
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
