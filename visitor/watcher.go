package visitor

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	log "github.com/sirupsen/logrus"
)

const (
	START     = "start" // Container action
	STOP      = "stop"  // Container action
	DIE       = "die"   // Container action
	DESTROY   = "destroy"
	CONTAINER = "container"
	EVENT     = "event"
)

type query struct {
	visitor func(string, *types.ContainerJSON)
	labels  []string
}

type Watcher struct {
	client     *client.Client
	queries    []*query
	containers map[string]*types.ContainerJSON
	visitors   []func(*types.ContainerJSON) error
	again      bool
	lock       sync.RWMutex
	ready      sync.WaitGroup
}

// New Watcher, from a Docker client
func New(client *client.Client) *Watcher {
	w := &Watcher{
		client:     client,
		queries:    make([]*query, 0),
		visitors:   make([]func(*types.ContainerJSON) error, 0),
		containers: make(map[string]*types.ContainerJSON),
	}
	w.ready.Add(1) // see Watcher.Ready()
	return w
}

// WatchFor visitor function and label names
func (w *Watcher) WatchFor(visitor func(action string, container *types.ContainerJSON), labels ...string) {
	w.queries = append(w.queries, &query{
		visitor: visitor,
		labels:  labels,
	})
}

// VisitCurrentCointainer visit already present containers
func (w *Watcher) VisitCurrentCointainer(visitor func(container *types.ContainerJSON) error) {
	w.visitors = append(w.visitors, visitor)
}

// loop over visitors, filter event with their labels, run asynchronously visitor
func (w *Watcher) trigger(action string, container *types.ContainerJSON) {
	log.WithField("action", action).WithField("id", container.ID).Debug("trigger")
	for _, query := range w.queries {
		if len(query.labels) == 0 {
			go query.visitor(action, container)
			continue
		}
		for _, label := range query.labels {
			if _, ok := container.Config.Labels[label]; ok {
				go query.visitor(action, container)
			}
		}
	}
}

// Look for already here containers
func (w *Watcher) init() error {
	containers, err := w.client.ContainerList(context.Background(), types.ContainerListOptions{
		All: true,
	})
	if err != nil {
		return err
	}
	log.WithField("n", len(containers)).Info("Initial state")
	for _, container := range containers {
		containerJSON, err := w.client.ContainerInspect(context.Background(), container.ID)
		if err != nil {
			return err
		}
		for _, v := range w.visitors {
			err = v(&containerJSON)
			if err != nil {
				return err
			}
		}
		log.WithField("id", container.ID).WithField("container", containerJSON).Debug("Old container")
		w.lock.Lock()
		w.containers[container.ID] = &containerJSON
		w.lock.Unlock()
	}
	w.ready.Done()
	return nil
}

// Ready when already here containers are known
func (w *Watcher) Ready() {
	w.ready.Wait()
}

// Start watching Docker events
func (w *Watcher) Start(ctx context.Context) error {
	w.again = true
	args := filters.NewArgs()
	// See https://docs.docker.com/engine/reference/commandline/events/#extended-description
	args.Add(EVENT, START)
	args.Add(EVENT, STOP)
	args.Add(EVENT, DIE)
	args.Add(EVENT, DESTROY)

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
		ctx2, cancel := context.WithCancel(context.Background())
		messages, errors := w.client.Events(ctx2, types.EventsOptions{
			Filters: args,
		})
		defer cancel()
		log.Info("Listening Docker messages")
		again := true
		for again {
			select {
			case <-ctx.Done():
				return nil
			case msg := <-messages:
				raw, _ := json.Marshal(msg)
				log.WithFields(log.Fields{
					"body":   string(raw),
					"action": msg.Action,
				}).Debug("Docker message")
				if msg.Action == START {
					container, err := w.client.ContainerInspect(context.Background(), msg.ID)
					if err != nil {
						log.Error(err)
					} else {
						w.lock.Lock()
						w.containers[container.ID] = &container
						w.lock.Unlock()
					}
					w.trigger(msg.Action, &container)
					continue
				}
				w.lock.RLock()
				c, ok := w.containers[msg.ID]
				if ok {
					w.trigger(msg.Action, c)
				} else {
					log.Error(msg.Action, msg.ID)
				}
				w.lock.RUnlock()
				if msg.Action == DESTROY {
					w.lock.Lock()
					delete(w.containers, msg.ID)
					w.lock.Unlock()
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
		w.ready.Add(1)
	}
	return nil
}

func (w *Watcher) Container(id string) *types.ContainerJSON {
	w.lock.RLock()
	defer w.lock.RUnlock()
	return w.containers[id]
}

func (w *Watcher) Find(visitor func(*types.ContainerJSON) (bool, error)) ([]*types.ContainerJSON, error) {
	r := make([]*types.ContainerJSON, 0)
	w.lock.Lock()
	defer w.lock.Unlock()
	for _, container := range w.containers {
		ok, err := visitor(container)
		if err != nil {
			return nil, err
		}
		if ok {
			r = append(r, container)
		}
	}
	return r, nil
}
