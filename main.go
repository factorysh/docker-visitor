package main

import (
	"fmt"

	"context"

	"github.com/davecgh/go-spew/spew"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/factorysh/docker-visitor/visitor"
	"github.com/onrik/logrus/filename"
	log "github.com/sirupsen/logrus"
)

// It's a debug tool, not a real main
func main() {
	filenameHook := filename.NewHook()
	log.AddHook(filenameHook)
	log.SetLevel(log.DebugLevel)
	c, err := client.NewEnvClient()
	if err != nil {
		panic(err)
	}
	w := visitor.New(c)
	w.WatchFor(func(action string, container *types.ContainerJSON) {
		fmt.Println("🐳 ", action)
		spew.Dump(container.State)
	})
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	err = w.Start(ctx)
	defer cancel()
	if err != nil {
		panic(err)
	}
}
