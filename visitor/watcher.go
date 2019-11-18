package visitor

import (
	"context"
	"encoding/json"
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
	log.WithField("n", len(containers)).Info("Initial state")
	for _, container := range containers {
		containerJSON, err := w.client.ContainerInspect(context.Background(), container.ID)
		if err != nil {
			return err
		}
		log.WithField("id", container.ID).WithField("container", containerJSON).Debug("Old container")
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
		log.Info("Docker ping")
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
