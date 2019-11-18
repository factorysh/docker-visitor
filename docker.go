package docker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	log "github.com/sirupsen/logrus"
)

const (
	DOCKER_TRIES = 12
	START        = "start" // Container action
	STOP         = "stop"  // Container action
	DIE          = "die"   // Container action
	CONTAINER    = "container"
	EVENT        = "event"
)

// PingDocker ping Docker, try 12 times, wait 5s
func PingDocker(_client *client.Client) error {
	for i := 0; i < DOCKER_TRIES; i++ { // Waiting for docker ping with a wait loop
		_, err := _client.Ping(context.Background())
		if err == nil {
			break
		}
		log.WithField("try", i).Error(err)
		if i == (DOCKER_TRIES - 1) {
			return errors.New("Timeout, can't connect to Docker	")
		}
		time.Sleep(5 * time.Second)
	}
	return nil
}

type query struct {
	visitor func(*types.ContainerJSON)
	labels  []string
}

type Watcher struct {
	client     *client.Client
	queries    []*query
	containers map[string]*types.ContainerJSON
	cancel     context.CancelFunc
	again      bool
}

func New(client *client.Client) *Watcher {
	return &Watcher{
		client:     client,
		queries:    make([]*query, 0),
		containers: make(map[string]*types.ContainerJSON),
	}
}

func (w *Watcher) WatchFor(visitor func(container *types.ContainerJSON), labels ...string) {
	w.queries = append(w.queries, &query{
		visitor: visitor,
		labels:  labels,
	})
}

func (w *Watcher) watch(container *types.ContainerJSON) {
	for _, query := range w.queries {
		for _, label := range query.labels {
			if _, ok := container.Config.Labels[label]; ok {
				go query.visitor(container)
			}
		}
	}
}

func (w *Watcher) init() error {
	containers, err := w.client.ContainerList(context.Background(), types.ContainerListOptions{})
	if err != nil {
		return err
	}
	for _, container := range containers {
		containerJSON, err := w.client.ContainerInspect(context.Background(), container.ID)
		if err != nil {
			return err
		}
		w.containers[container.ID] = &containerJSON
		w.watch(&containerJSON)
	}
	return nil
}

func (w *Watcher) Start(cancel context.CancelFunc) error {
	w.cancel = cancel
	w.again = true
	for w.again {
		err := PingDocker(w.client)
		if err != nil {
			return err
		}
		err = w.init()
		if err != nil {
			return err
		}
		var ctx context.Context
		ctx, w.cancel = context.WithCancel(context.Background())
		args := filters.NewArgs()
		// See https://docs.docker.com/engine/reference/commandline/events/#extended-description
		args.Add(EVENT, START)
		args.Add(EVENT, STOP)
		args.Add(EVENT, DIE)

		messages, errors := w.client.Events(ctx, types.EventsOptions{
			Filters: args,
		})
		log.Info("Listening Docker messages")
		again := true
		for again {
			select {
			case msg := <-messages:
				raw, _ := json.Marshal(msg)
				log.WithFields(log.Fields{
					"body":   string(raw),
					"action": msg.Action,
				}).Debug("Docker message")
				if msg.Action == STOP || msg.Action == DIE {
					delete(w.containers, msg.ID)
				}
				if msg.Action == START {
					container, err := w.client.ContainerInspect(context.Background(), msg.ID)
					if err != nil {
						log.Error(err)
					} else {
						w.containers[container.ID] = &container
					}
				}

			case err := <-errors:
				log.WithError(err).Error("Docker event error")
				switch err {
				case io.EOF: // Docker cut the stream
					again = false
				case nil: // FIXME what happened? lets reboot
					again = false
				}
				time.Sleep(10 * time.Second) // Don't flood log
			}
		}
	}
	return nil
}
