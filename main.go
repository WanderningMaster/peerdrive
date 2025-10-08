package main

import (
	"fmt"

	"github.com/WanderningMaster/peerdrive/configuration"
)

func main() {
	cfg := configuration.LoadUserConfig()
	fmt.Println(cfg)
}
