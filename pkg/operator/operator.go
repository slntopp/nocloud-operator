package operator

import (
	"context"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	dockerClient "github.com/docker/docker/client"
	log "github.com/sirupsen/logrus"
)

const (
	TYPE_CONTAINER = "container"
	ACTION_START   = "start"
	ACTION_STOP    = "stop"
)

type Operator struct {
	client *dockerClient.Client
}

func NewOperator() *Operator {
	cli, err := dockerClient.NewClientWithOpts(dockerClient.FromEnv, dockerClient.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatal(err)
	}
	return &Operator{client: cli}
}

func (o *Operator) Ps() []ContainerInfo {
	ctx := context.Background()
	containers, err := o.client.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		log.Fatal(err)
	}

	var result []ContainerInfo

	for _, container := range containers {
		result = append(result, *NewContainerInfo(&container))
	}
	return result
}

func (o *Operator) ObserveContainers() {
	ctx := context.Background()
	eventsChan, errorsChan := o.client.Events(ctx, types.EventsOptions{})
	for {
		select {
		case event := <-eventsChan:
			if event.Type == TYPE_CONTAINER && (event.Action == ACTION_START || event.Action == ACTION_STOP) {
				processEvent(ctx, o.client, event)
			}
		case err := <-errorsChan:
			fmt.Println(err.Error())
		default:
			continue
		}
	}
}

func processEvent(ctx context.Context, client *dockerClient.Client, event events.Message) {
	if event.Action == ACTION_STOP {
		log.Println("Container stopped")
		log.Println("ID: " + event.ID)
		return
	}

	containers, err := client.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		log.Fatal("Error")
	}
	var container *ContainerInfo
	for _, value := range containers {
		if value.ID == event.ID {
			container = NewContainerInfo(&value)
		}
	}

	log.Println("Container started")
	log.Println(container)
	return
}
