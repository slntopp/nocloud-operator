package operator

import (
	"context"
	"github.com/docker/docker/api/types"
	dockerClient "github.com/docker/docker/client"
	log "github.com/sirupsen/logrus"
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
