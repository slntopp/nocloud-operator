package operator

import (
	"context"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	dockerFilters "github.com/docker/docker/api/types/filters"
	dockerClient "github.com/docker/docker/client"
	log "github.com/sirupsen/logrus"
	"io"
	"os"
	"strings"
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
	container := NewContainerInfo(&containers[0])
	o.containers[container.Id] = *container
	log.Println("Container started")
	log.Println(container)
	o.CheckHash(ctx, container.Id)
	return
}

func (o *Operator) CheckHash(ctx context.Context, containerId string) {
	container, _, err := o.client.ContainerInspectWithRaw(ctx, containerId, false)
	if err != nil {
		return
	}

	image, _, err := o.client.ImageInspectWithRaw(ctx, container.Image)
	if err != nil {
		return
	}

	imageFullName := image.RepoTags[0]
	imageName := getImageName(imageFullName)
	o.pullImage(ctx, imageName)
	o.updateImageAndContainer(ctx, imageName, containerId)
}

func (o *Operator) pullImage(ctx context.Context, imageName string) {
	out, err := o.client.ImagePull(ctx, imageName, types.ImagePullOptions{})
	if err != nil {
		panic(err)
	}

	defer out.Close()

	io.Copy(os.Stdout, out)
}

func getImageName(imageFullName string) string {
	splitedName := strings.Split(imageFullName, ":")
	return splitedName[0]
}

func getImageTag(imageFullName string) string {
	splitedName := strings.Split(imageFullName, ":")
	return splitedName[1]
}

func (o *Operator) updateImageAndContainer(ctx context.Context, imageName string, containerId string) {
	images := o.getImages(ctx, imageName)
	if len(images) == 0 {
		log.Println("Container has latest image")
	}

	var oldImage types.ImageSummary

	for _, image := range images {
		tag := getImageTag(image.RepoTags[0])
		if tag == "latest" {
			continue
		}
		oldImage = image
	}

	err := o.client.ContainerRemove(ctx, containerId, types.ContainerRemoveOptions{})
	if err != nil {
		return
	}

	_, err = o.client.ImageRemove(ctx, oldImage.ID, types.ImageRemoveOptions{})
	if err != nil {
		return
	}
}

func (o *Operator) getImages(ctx context.Context, imageName string) []types.ImageSummary {
	filters := dockerFilters.NewArgs()
	filters.Add("reference", imageName)
	images, err := o.client.ImageList(ctx, types.ImageListOptions{Filters: filters})
	if err != nil {
		log.Fatal(err)
	}
	return images
}
