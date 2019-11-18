package visitor

import (
	"context"
	"errors"
	"time"

	"github.com/docker/docker/client"
	log "github.com/sirupsen/logrus"
)

var (
	// DockerTries is the number of retries
	DockerTries = 12
)

// PingDocker ping Docker, try 12 times, wait 5s
func PingDocker(_client *client.Client) error {
	for i := 0; i < DockerTries; i++ { // Waiting for docker ping with a wait loop
		_, err := _client.Ping(context.Background())
		if err == nil {
			break
		}
		log.WithField("try", i).Error(err)
		if i == (DockerTries - 1) {
			return errors.New("Timeout, can't connect to Docker	")
		}
		time.Sleep(5 * time.Second)
	}
	return nil
}
