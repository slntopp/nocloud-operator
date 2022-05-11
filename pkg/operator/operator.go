package operator

import (
	"context"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	dockerFilters "github.com/docker/docker/api/types/filters"
	dockerClient "github.com/docker/docker/client"
	log "github.com/sirupsen/logrus"
)

const (
	TYPE_CONTAINER = "container"
	ACTION_START   = "start"
	ACTION_STOP    = "stop"
)

type Operator struct {
	client     *dockerClient.Client
	containers map[string]ContainerInfo
}

func NewOperator() *Operator {
	cli, err := dockerClient.NewClientWithOpts(dockerClient.FromEnv, dockerClient.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatal(err)
	}
	return &Operator{client: cli, containers: map[string]ContainerInfo{}}
}

func (o *Operator) Ps() map[string]ContainerInfo {
	ctx := context.Background()
	containers, err := o.client.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		log.Fatal(err)
	}

	for _, container := range containers {
		o.containers[container.ID] = *NewContainerInfo(&container)
	}
	return o.containers
}

func (o *Operator) ObserveContainers() {
	ctx := context.Background()
	eventsChan, errorsChan := o.client.Events(ctx, types.EventsOptions{})
	for {
		select {
		case event := <-eventsChan:
			if event.Type == TYPE_CONTAINER && (event.Action == ACTION_START || event.Action == ACTION_STOP) {
				o.processEvent(ctx, event)
			}
		case err := <-errorsChan:
			fmt.Println(err.Error())
		default:
			continue
		}
	}
}

func (o *Operator) processEvent(ctx context.Context, event events.Message) {
	if event.Action == ACTION_STOP {
		names := o.containers[event.ID].Names
		log.Printf("Container stopped ID: %s Names:%v", event.ID, names)
		delete(o.containers, event.ID)
		return
	}

	filters := dockerFilters.NewArgs()
	filters.Add("id", event.ID)

	containers, err := o.client.ContainerList(ctx, types.ContainerListOptions{
		Filters: filters,
	})
	if err != nil {
		log.Fatal("Error")
	}
	var container *ContainerInfo
	container = NewContainerInfo(&containers[0])
	o.containers[container.Id] = *container
	log.Println("Container started")
	log.Println(container)
	return
}
