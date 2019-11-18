package main

import (
	"fmt"

	"context"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/factorysh/docker-visitor/visitor"
)

// It's a debug tool, not a real main
func main() {
	c, err := client.NewEnvClient()
	if err != nil {
		panic(err)
	}
	w := visitor.New(c)
	w.WatchFor(func(container *types.ContainerJSON) {
		fmt.Println(container)
	})
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	err = w.Start(cancel)
	if err != nil {
		panic(err)
	}
}
