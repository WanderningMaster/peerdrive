package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/WanderningMaster/peerdrive/configuration"
	"github.com/WanderningMaster/peerdrive/internal/routing"
	"github.com/WanderningMaster/peerdrive/internal/service"
)

func kvPut(key, value string) {
	conf := configuration.LoadUserConfig()
	if key == "" {
		log.Fatal("-key is required")
	}
	u, err := url.Parse(fmt.Sprintf("http://0.0.0.0:%d", conf.HttpPort))
	if err != nil {
		log.Fatal(err)
	}
	u.Path = "/put"
	q := u.Query()
	q.Set("key", key)
	q.Set("value", value)
	u.RawQuery = q.Encode()
	resp, err := http.Post(u.String(), "application/json", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	printOk(resp)
}

func kvGet(key string) {
	conf := configuration.LoadUserConfig()
	if key == "" {
		log.Fatal("-key is required")
	}
	u, err := url.Parse(fmt.Sprintf("http://0.0.0.0:%d", conf.HttpPort))
	if err != nil {
		log.Fatal(err)
	}
	u.Path = "/get"
	q := u.Query()
	q.Set("key", key)
	u.RawQuery = q.Encode()
	resp, err := http.Get(u.String())
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	printGet(resp)
}

func dfsPut(inPath string) {
	conf := configuration.LoadUserConfig()
	if inPath == "" {
		log.Fatal("-in is required")
	}
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

func dfsGet(cid, out string) {
	conf := configuration.LoadUserConfig()
	if cid == "" {
		log.Fatal("-cid is required")
	}
	u, err := url.Parse(fmt.Sprintf("http://0.0.0.0:%d", conf.HttpPort))
	if err != nil {
		log.Fatal(err)
	}
	u.Path = "/dfs/" + cid

	resp, err := http.Get(u.String())
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		log.Fatalf("server error %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	if out != "" {
		outPath := out
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
	if _, err := io.Copy(os.Stdout, resp.Body); err != nil {
		log.Fatal(err)
	}
}

func bootstrap(peers string) {
	conf := configuration.LoadUserConfig()
	if peers == "" {
		log.Fatal("-peers is required")
	}
	u, err := url.Parse(fmt.Sprintf("http://0.0.0.0:%d", conf.HttpPort))
	if err != nil {
		log.Fatal(err)
	}
	u.Path = "/bootstrap"
	q := u.Query()
	q.Set("peers", peers)
	u.RawQuery = q.Encode()

	resp, err := http.Post(u.String(), "application/json", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	printBootstrap(resp)
}

func pins() {
	conf := configuration.LoadUserConfig()
	u, err := url.Parse(fmt.Sprintf("http://0.0.0.0:%d", conf.HttpPort))
	if err != nil {
		log.Fatal(err)
	}
	u.Path = "/pins"
	resp, err := http.Get(u.String())
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	printPins(resp)
}

func pin(cid string) {
	conf := configuration.LoadUserConfig()
	if cid == "" {
		log.Fatal("-cid is required")
	}
	u, err := url.Parse(fmt.Sprintf("http://0.0.0.0:%d", conf.HttpPort))
	if err != nil {
		log.Fatal(err)
	}
	u.Path = "/pin"
	q := u.Query()
	q.Set("cid", cid)
	u.RawQuery = q.Encode()
	resp, err := http.Post(u.String(), "application/json", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	printOk(resp)
}

func unpin(cid string) {
	conf := configuration.LoadUserConfig()
	if cid == "" {
		log.Fatal("-cid is required")
	}
	u, err := url.Parse(fmt.Sprintf("http://0.0.0.0:%d", conf.HttpPort))
	if err != nil {
		log.Fatal(err)
	}
	u.Path = "/unpin"
	q := u.Query()
	q.Set("cid", cid)
	u.RawQuery = q.Encode()
	resp, err := http.Post(u.String(), "application/json", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	printOk(resp)
}

func closest(target string, k int) {
	conf := configuration.LoadUserConfig()
	u, err := url.Parse(fmt.Sprintf("http://0.0.0.0:%d", conf.HttpPort))
	if err != nil {
		log.Fatal(err)
	}
	u.Path = "/closest"
	q := u.Query()
	if target != "" {
		q.Set("target", target)
	}
	if k > 0 {
		q.Set("k", fmt.Sprintf("%d", k))
	}
	u.RawQuery = q.Encode()

	resp, err := http.Get(u.String())
	if err != nil {
		log.Fatal(err)
	}
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
	if err != nil {
		log.Fatal(err)
	}
	var m struct {
		Ok bool `json:"ok"`
	}
	if json.Unmarshal(b, &m) == nil && m.Ok {
		fmt.Println("ok")
		return
	}
	fmt.Println(string(b))
}

func printGet(resp *http.Response) {
	b, err := readAndCheck(resp)
	if err != nil {
		log.Fatal(err)
	}
	var m struct {
		Found bool   `json:"found"`
		Value string `json:"value"`
	}
	if json.Unmarshal(b, &m) == nil && m.Found {
		fmt.Println(m.Value)
		return
	}
	fmt.Println(string(b))
}

func printDfsPut(resp *http.Response) {
	b, err := readAndCheck(resp)
	if err != nil {
		log.Fatal(err)
	}
	var m struct {
		CID string `json:"cid"`
	}
	if json.Unmarshal(b, &m) == nil && m.CID != "" {
		fmt.Println(m.CID)
		return
	}
	fmt.Println(string(b))
}

func printBootstrap(resp *http.Response) {
	b, err := readAndCheck(resp)
	if err != nil {
		log.Fatal(err)
	}
	var m struct {
		Ok    bool     `json:"ok"`
		Peers []string `json:"peers"`
	}
	if json.Unmarshal(b, &m) == nil && m.Ok {
		if len(m.Peers) == 0 {
			fmt.Println("ok")
		} else {
			fmt.Printf("ok: %d peers\n", len(m.Peers))
			for _, p := range m.Peers {
				fmt.Println("-", p)
			}
		}
		return
	}
	fmt.Println(string(b))
}

func printPins(resp *http.Response) {
	b, err := readAndCheck(resp)
	if err != nil {
		log.Fatal(err)
	}
	var arr []service.PinInfo
	if json.Unmarshal(b, &arr) == nil {
		tw := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
		fmt.Fprintln(tw, "CID\tNAME\tSIZE\tMIME")
		for _, p := range arr {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", p.CID, p.Name, p.Size, p.Mime)
		}
		_ = tw.Flush()
		return
	}
	fmt.Println(string(b))
}

func printClosest(resp *http.Response) {
    b, err := readAndCheck(resp)
    if err != nil {
        log.Fatal(err)
    }
    var arr []routing.Contact
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

func gc() {
    conf := configuration.LoadUserConfig()
    u, err := url.Parse(fmt.Sprintf("http://0.0.0.0:%d", conf.HttpPort))
    if err != nil {
        log.Fatal(err)
    }
    u.Path = "/gc"
    resp, err := http.Post(u.String(), "application/json", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    printGC(resp)
}

func printGC(resp *http.Response) {
    b, err := readAndCheck(resp)
    if err != nil {
        log.Fatal(err)
    }
    var m struct{ Freed int `json:"freed"` }
    if json.Unmarshal(b, &m) == nil {
        fmt.Printf("freed %d blocks\n", m.Freed)
        return
    }
    fmt.Println(string(b))
}
