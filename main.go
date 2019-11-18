package main

import (
	"fmt"

	"context"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/factorysh/docker-visitor/visitor"
	log "github.com/sirupsen/logrus"
)

// It's a debug tool, not a real main
func main() {
	log.SetLevel(log.DebugLevel)
	c, err := client.NewEnvClient()
	if err != nil {
		panic(err)
	}
	w := visitor.New(c)
	w.WatchFor(func(action string, container *types.ContainerJSON) {
		fmt.Println("🐳 ", action, container)
	})
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	err = w.Start(cancel)
	if err != nil {
		panic(err)
	}
}
