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
    "encoding/json"
    "text/tabwriter"

    api "github.com/WanderningMaster/peerdrive/api"
	"github.com/WanderningMaster/peerdrive/configuration"
	"github.com/WanderningMaster/peerdrive/internal/relay"
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
    case "relay":
        serveRelay(os.Args[2:])
    case "kv":
        if len(os.Args) < 3 {
            fmt.Println("usage: peerdrive kv <add|get> [flags]")
            os.Exit(2)
        }
        switch os.Args[2] {
        case "add":
            putHTTP(os.Args[3:])
        case "get":
            getHTTP(os.Args[3:])
        default:
            fmt.Println("usage: peerdrive kv <add|get> [flags]")
            os.Exit(2)
        }
    case "add":
        dfsPutHTTP(os.Args[2:])
    case "get":
        dfsGetHTTP(os.Args[2:])
    case "bootstrap":
        bootstrapHTTP(os.Args[2:])
    case "pins":
        pinsHTTP(os.Args[2:])
    case "pin":
        pinHTTP(os.Args[2:])
    case "unpin":
        unpinHTTP(os.Args[2:])
    case "closest":
        closestHTTP(os.Args[2:])
    default:
        usage()
        os.Exit(2)
    }
}

func usage() {
    fmt.Println(`peerdrive <command> [flags]
Commands:
init Run a node and start the built-in HTTP Client (/id, /put, /get, /dfs/{cid}, /dfs/put)
relay Run an inbound relay server for attached nodes
kv add Store key/value on a running node
kv get Fetch value from a running node
add Add file to DFS from path; prints resulting CID
get Fetch DFS content by CID; write to -out or stdout (pipe)
bootstrap Connect a running node to comma-separated peers
pins List pinned CIDs from the running node
pin Pin a CID in the running node
unpin Unpin a CID in the running node
closest List closest contacts (optionally -target and -k)
Use -h after a command for flags.`)
}

func serveHTTP(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	boot := fs.String("bootstrap", "", "comma-separated peers to bootstrap (host:port)")
	relayAddr := fs.String("relay", "", "relay server addr (host:port) to attach")
	mem := fs.Bool("mem", false, "use in-mem blockstore; defaults to on-disk")

	fs.Parse(args)

	conf := configuration.LoadUserConfig()
    api.BootstrapHttpClient(conf, boot, relayAddr, mem)

}

func serveRelay(args []string) {
	fs := flag.NewFlagSet("relay", flag.ExitOnError)
	listen := fs.String("listen", ":35000", "address to listen for relay server (host:port)")
	fs.Parse(args)

	srv := relay.NewServer()
	if err := srv.ListenAndServe(*listen); err != nil {
		log.Fatal(err)
	}
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

	resp, err := http.Post(u.String(), "application/json", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	printOk(resp)
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
	printGet(resp)
}

func dfsPutHTTP(args []string) {
    conf := configuration.LoadUserConfig()

    fs := flag.NewFlagSet("add", flag.ExitOnError)
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

	resp, err := http.Post(u.String(), "application/json", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	printDfsPut(resp)
}

func dfsGetHTTP(args []string) {
    conf := configuration.LoadUserConfig()

    fs := flag.NewFlagSet("get", flag.ExitOnError)
    cid := fs.String("cid", "", "CID of the DFS manifest to fetch")
    out := fs.String("out", "", "optional output file path; if omitted, writes to stdout")
    fs.Parse(args)
    if *cid == "" {
        log.Fatal("-cid is required")
    }

    u, err := url.Parse(fmt.Sprintf("http://0.0.0.0:%d", conf.HttpPort))
    if err != nil {
        log.Fatal(err)
    }
    u.Path = "/dfs/" + *cid

    resp, err := http.Get(u.String())
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    if resp.StatusCode != 200 {
        // read error as JSON/message
        b, _ := io.ReadAll(resp.Body)
        log.Fatalf("server error %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
    }

    if *out != "" {
        outPath := *out
        if !filepath.IsAbs(outPath) {
            cwd, err := os.Getwd()
            if err != nil {
                log.Fatal(err)
            }
            outPath = filepath.Clean(filepath.Join(cwd, outPath))
        }
        f, err := os.Create(outPath)
        if err != nil {
            log.Fatal(err)
        }
        defer f.Close()
        if _, err := io.Copy(f, resp.Body); err != nil {
            log.Fatal(err)
        }
        return
    }
    // stream to stdout for piping
    if _, err := io.Copy(os.Stdout, resp.Body); err != nil {
        log.Fatal(err)
    }
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

	resp, err := http.Post(u.String(), "application/json", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	printBootstrap(resp)
}

func pinsHTTP(args []string) {
    conf := configuration.LoadUserConfig()
    u, err := url.Parse(fmt.Sprintf("http://0.0.0.0:%d", conf.HttpPort))
    if err != nil { log.Fatal(err) }
    u.Path = "/pins"
    resp, err := http.Get(u.String())
    if err != nil { log.Fatal(err) }
    defer resp.Body.Close()
    printPins(resp)
}

func pinHTTP(args []string) {
    conf := configuration.LoadUserConfig()
    fs := flag.NewFlagSet("pin", flag.ExitOnError)
    cid := fs.String("cid", "", "CID to pin")
    fs.Parse(args)
    if *cid == "" { log.Fatal("-cid is required") }
    u, err := url.Parse(fmt.Sprintf("http://0.0.0.0:%d", conf.HttpPort))
    if err != nil { log.Fatal(err) }
    u.Path = "/pin"
    q := u.Query()
    q.Set("cid", *cid)
    u.RawQuery = q.Encode()
    resp, err := http.Post(u.String(), "application/json", nil)
    if err != nil { log.Fatal(err) }
    defer resp.Body.Close()
    printOk(resp)
}

func unpinHTTP(args []string) {
    conf := configuration.LoadUserConfig()
    fs := flag.NewFlagSet("unpin", flag.ExitOnError)
    cid := fs.String("cid", "", "CID to unpin")
    fs.Parse(args)
    if *cid == "" { log.Fatal("-cid is required") }
    u, err := url.Parse(fmt.Sprintf("http://0.0.0.0:%d", conf.HttpPort))
    if err != nil { log.Fatal(err) }
    u.Path = "/unpin"
    q := u.Query()
    q.Set("cid", *cid)
    u.RawQuery = q.Encode()
    resp, err := http.Post(u.String(), "application/json", nil)
    if err != nil { log.Fatal(err) }
    defer resp.Body.Close()
    printOk(resp)
}

func closestHTTP(args []string) {
    conf := configuration.LoadUserConfig()
    fs := flag.NewFlagSet("closest", flag.ExitOnError)
    target := fs.String("target", "", "hex NodeID to use as target (defaults to self)")
    k := fs.Int("k", 0, "number of contacts to return (defaults to server conf)")
    fs.Parse(args)

    u, err := url.Parse(fmt.Sprintf("http://0.0.0.0:%d", conf.HttpPort))
    if err != nil { log.Fatal(err) }
    u.Path = "/closest"
    q := u.Query()
    if *target != "" { q.Set("target", *target) }
    if *k > 0 { q.Set("k", fmt.Sprintf("%d", *k)) }
    u.RawQuery = q.Encode()

    resp, err := http.Get(u.String())
    if err != nil { log.Fatal(err) }
    defer resp.Body.Close()
    printClosest(resp)
}

func readAndCheck(resp *http.Response) ([]byte, error) {
    b, _ := io.ReadAll(resp.Body)
    if resp.StatusCode != 200 {
        var e any
        if json.Unmarshal(b, &e) == nil {
            pretty, _ := json.MarshalIndent(e, "", "  ")
            return nil, fmt.Errorf("server error %d:\n%s", resp.StatusCode, string(pretty))
        }
        return nil, fmt.Errorf("server error %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
    }
    return b, nil
}

func printOk(resp *http.Response) {
    b, err := readAndCheck(resp)
    if err != nil { log.Fatal(err) }
    var m struct{ Ok bool `json:"ok"` }
    if json.Unmarshal(b, &m) == nil && m.Ok {
        fmt.Println("ok")
        return
    }
    fmt.Println(string(b))
}

func printGet(resp *http.Response) {
    b, err := readAndCheck(resp)
    if err != nil { log.Fatal(err) }
    var m struct{ Found bool `json:"found"`; Value string `json:"value"` }
    if json.Unmarshal(b, &m) == nil && m.Found {
        fmt.Println(m.Value)
        return
    }
    fmt.Println(string(b))
}

func printDfsPut(resp *http.Response) {
    b, err := readAndCheck(resp)
    if err != nil { log.Fatal(err) }
    var m struct{ CID string `json:"cid"` }
    if json.Unmarshal(b, &m) == nil && m.CID != "" {
        fmt.Println(m.CID)
        return
    }
    fmt.Println(string(b))
}

func printDfsGet(resp *http.Response) {
    b, err := readAndCheck(resp)
    if err != nil { log.Fatal(err) }
    var m struct{ Ok bool `json:"ok"`; Out string `json:"out"`; Bytes int `json:"bytes"` }
    if json.Unmarshal(b, &m) == nil && m.Ok {
        fmt.Printf("wrote %d bytes to %s\n", m.Bytes, m.Out)
        return
    }
    fmt.Println(string(b))
}

func printBootstrap(resp *http.Response) {
    b, err := readAndCheck(resp)
    if err != nil { log.Fatal(err) }
    var m struct{ Ok bool `json:"ok"`; Peers []string `json:"peers"` }
    if json.Unmarshal(b, &m) == nil && m.Ok {
        if len(m.Peers) == 0 {
            fmt.Println("ok")
        } else {
            fmt.Printf("ok: %d peers\n", len(m.Peers))
            for _, p := range m.Peers { fmt.Println("-", p) }
        }
        return
    }
    fmt.Println(string(b))
}

func printPins(resp *http.Response) {
    b, err := readAndCheck(resp)
    if err != nil { log.Fatal(err) }
    type pinInfo struct{ CID, Name, Mime string }
    var arr []pinInfo
    if json.Unmarshal(b, &arr) == nil {
        tw := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
        fmt.Fprintln(tw, "CID\tNAME\tMIME")
        for _, p := range arr {
            fmt.Fprintf(tw, "%s\t%s\t%s\n", p.CID, p.Name, p.Mime)
        }
        _ = tw.Flush()
        return
    }
    fmt.Println(string(b))
}

func printClosest(resp *http.Response) {
    b, err := readAndCheck(resp)
    if err != nil { log.Fatal(err) }
    type contact struct{ ID, Addr, Relay string }
    var arr []contact
    if json.Unmarshal(b, &arr) == nil {
        tw := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
        fmt.Fprintln(tw, "ID\tADDR\tRELAY")
        for _, c := range arr {
            fmt.Fprintf(tw, "%s\t%s\t%s\n", c.ID, c.Addr, c.Relay)
        }
        _ = tw.Flush()
        return
    }
    fmt.Println(string(b))
}
