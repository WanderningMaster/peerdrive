package configuration

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"os"
	"path"
	"time"

	"github.com/WanderningMaster/peerdrive/internal/id"
)

type UserConfig struct {
	NodeId   id.NodeID `json:"nodeId"`
	TcpPort  int       `json:"tcpPort"`
	HttpPort int       `json:"httpPort"`
	Relay    string    `json:"relay,omitempty"`
}

func getRandomPort(minPort, maxPort int) (int, error) {
	rand.Seed(time.Now().UnixNano())
	portRange := maxPort - minPort + 1

	for i := 0; i < 30; i++ { // try up to 30 times
		port := rand.Intn(portRange) + minPort
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			_ = ln.Close()
			return port, nil
		}
	}
	return 0, fmt.Errorf("could not find free port in range %d–%d", minPort, maxPort)
}

func LoadUserConfig() *UserConfig {
	configDir, err := os.UserConfigDir()
	if err != nil {
		panic(err)
	}

	cfgDir := path.Join(configDir, "peerdrive")
	cfgPath := path.Join(cfgDir, "config.json")
	f, err := os.Open(cfgPath)
	if err != nil {
		cfg := defaultUserConfig()
		_ = os.MkdirAll(cfgDir, 0o755)
		if data, mErr := json.MarshalIndent(cfg, "", "  "); mErr == nil {
			_ = os.WriteFile(cfgPath, data, 0o644)
		}
		return cfg
	}
	defer f.Close()

	var cfg UserConfig
	dec := json.NewDecoder(f)
	if err := dec.Decode(&cfg); err != nil {
		cfg2 := defaultUserConfig()
		_ = f.Close()
		_ = os.MkdirAll(cfgDir, 0o755)
		if data, mErr := json.MarshalIndent(cfg2, "", "  "); mErr == nil {
			_ = os.WriteFile(cfgPath, data, 0o644)
		}
		return cfg2
	}

	return &cfg
}

func defaultUserConfig() *UserConfig {
	tcpPort, err := getRandomPort(30000, 30100)
	if err != nil {
		panic(err)
	}

	httpPort := 8000 + tcpPort%1000

	return &UserConfig{
		TcpPort:  tcpPort,
		HttpPort: httpPort,
		NodeId:   id.RandomID(),
		// Relay:    "3.127.69.180:20018",
	}
}
