package main

import (
	"fmt"
	"log"
	"os"

	api "github.com/WanderningMaster/peerdrive/api"
	"github.com/WanderningMaster/peerdrive/configuration"
	"github.com/WanderningMaster/peerdrive/internal/relay"
	daemon "github.com/coreos/go-systemd/v22/daemon"
	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:           "peerdrive",
		Short:         "PeerDrive CLI",
		Long:          "PeerDrive command-line interface.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	var (
		initBootstrap string
		initRelay     string
		initMem       bool
	)
	cmdInit := &cobra.Command{
		Use:   "init",
		Short: "Run node with built-in HTTP client",
		RunE: func(cmd *cobra.Command, args []string) error {
			runInit(initBootstrap, initRelay, initMem)
			return nil
		},
	}
	cmdInit.Flags().StringVarP(&initBootstrap, "bootstrap", "b", "", "comma-separated peers to bootstrap (host:port)")
	cmdInit.Flags().StringVarP(&initRelay, "relay", "r", "", "relay server addr (host:port) to attach")
	cmdInit.Flags().BoolVarP(&initMem, "mem", "m", false, "use in-mem blockstore; defaults to on-disk")
	root.AddCommand(cmdInit)

	var relayListen string
	cmdRelay := &cobra.Command{
		Use:   "relay",
		Short: "Run inbound relay server",
		RunE: func(cmd *cobra.Command, args []string) error {
			runRelay(relayListen)
			return nil
		},
	}
	cmdRelay.Flags().StringVarP(&relayListen, "listen", "l", ":35000", "address to listen for relay server (host:port)")
	root.AddCommand(cmdRelay)

	cmdKV := &cobra.Command{Use: "kv", Short: "Key/value operations"}

	var kvAddKey, kvAddVal string
	cmdKVAdd := &cobra.Command{
		Use:   "add",
		Short: "Store key/value on a running node",
		RunE: func(cmd *cobra.Command, args []string) error {
			kvPut(kvAddKey, kvAddVal)
			return nil
		},
	}
	cmdKVAdd.Flags().StringVarP(&kvAddKey, "key", "k", "", "key (string; passed as-is to server)")
	cmdKVAdd.Flags().StringVarP(&kvAddVal, "value", "v", "", "value")
	_ = cmdKVAdd.MarkFlagRequired("key")
	cmdKV.AddCommand(cmdKVAdd)

	var kvGetKey string
	cmdKVGet := &cobra.Command{
		Use:   "get",
		Short: "Fetch value from a running node",
		RunE: func(cmd *cobra.Command, args []string) error {
			kvGet(kvGetKey)
			return nil
		},
	}
	cmdKVGet.Flags().StringVarP(&kvGetKey, "key", "k", "", "key (string; passed as-is to server)")
	_ = cmdKVGet.MarkFlagRequired("key")
	cmdKV.AddCommand(cmdKVGet)
	root.AddCommand(cmdKV)

	var addIn string
	cmdAdd := &cobra.Command{
		Use:   "add",
		Short: "Add file to DFS; prints CID",
		RunE: func(cmd *cobra.Command, args []string) error {
			dfsPut(addIn)
			return nil
		},
	}
	cmdAdd.Flags().StringVarP(&addIn, "in", "i", "", "input file path to add to DFS")
	_ = cmdAdd.MarkFlagRequired("in")
	root.AddCommand(cmdAdd)

	var getCID, getOut string
	cmdGet := &cobra.Command{
		Use:   "get",
		Short: "Fetch DFS content by CID",
		RunE: func(cmd *cobra.Command, args []string) error {
			dfsGet(getCID, getOut)
			return nil
		},
	}
	cmdGet.Flags().StringVarP(&getCID, "cid", "c", "", "CID of the DFS manifest to fetch")
	cmdGet.Flags().StringVarP(&getOut, "out", "o", "", "optional output file path; if omitted, writes to stdout")
	_ = cmdGet.MarkFlagRequired("cid")
	root.AddCommand(cmdGet)

	var bootstrapPeers string
	cmdBootstrap := &cobra.Command{
		Use:   "bootstrap",
		Short: "Connect a running node to peers",
		RunE: func(cmd *cobra.Command, args []string) error {
			bootstrap(bootstrapPeers)
			return nil
		},
	}
	cmdBootstrap.Flags().StringVarP(&bootstrapPeers, "peers", "p", "", "comma-separated peers to bootstrap (host:port)")
	_ = cmdBootstrap.MarkFlagRequired("peers")
	root.AddCommand(cmdBootstrap)

	cmdPins := &cobra.Command{
		Use:   "pins",
		Short: "List pinned CIDs",
		RunE: func(cmd *cobra.Command, args []string) error {
			pins()
			return nil
		},
	}
	root.AddCommand(cmdPins)

	var pinCID string
	cmdPin := &cobra.Command{
		Use:   "pin",
		Short: "Pin a CID",
		RunE: func(cmd *cobra.Command, args []string) error {
			pin(pinCID)
			return nil
		},
	}
	cmdPin.Flags().StringVarP(&pinCID, "cid", "c", "", "CID to pin")
	_ = cmdPin.MarkFlagRequired("cid")
	root.AddCommand(cmdPin)

	var unpinCID string
	cmdUnpin := &cobra.Command{
		Use:   "unpin",
		Short: "Unpin a CID",
		RunE: func(cmd *cobra.Command, args []string) error {
			unpin(unpinCID)
			return nil
		},
	}
	cmdUnpin.Flags().StringVarP(&unpinCID, "cid", "c", "", "CID to unpin")
	_ = cmdUnpin.MarkFlagRequired("cid")
	root.AddCommand(cmdUnpin)

	var closestTarget string
	var closestK int
	cmdClosest := &cobra.Command{
		Use:   "closest",
		Short: "List closest contacts",
		RunE: func(cmd *cobra.Command, args []string) error {
			closest(closestTarget, closestK)
			return nil
		},
	}
	cmdClosest.Flags().StringVarP(&closestTarget, "target", "t", "", "hex NodeID to use as target (defaults to self)")
	cmdClosest.Flags().IntVarP(&closestK, "k", "k", 0, "number of contacts to return (defaults to server conf)")
	root.AddCommand(cmdClosest)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runInit(bootstrap, relayAddr string, mem bool) {
	conf := configuration.LoadUserConfig()
	api.BootstrapHttpClient(conf, &bootstrap, &relayAddr, &mem)
}

func runRelay(listen string) {
	srv := relay.NewServer()
	go func() {
		if err := srv.ListenAndServe(listen); err != nil {
			log.Fatal(err)
		}
	}()

	_, _ = daemon.SdNotify(false, daemon.SdNotifyReady)

	select {}
}
