package operator

import (
	"context"
	"fmt"
	"github.com/docker/go-connections/nat"
	"gopkg.in/yaml.v3"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	dockerContainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	dockerFilters "github.com/docker/docker/api/types/filters"
	dockerClient "github.com/docker/docker/client"
	"github.com/gorobot-nz/docker-operator/pkg/parser"
	log "github.com/sirupsen/logrus"
)

const (
	TYPE_CONTAINER = "container"
	ACTION_START   = "start"
	ACTION_STOP    = "stop"
)

type Operator struct {
	client        *dockerClient.Client
	containers    map[string]ContainerInfo
	composeConfig parser.Config
}

func NewOperator() *Operator {
	cli, err := dockerClient.NewClientWithOpts(dockerClient.FromEnv, dockerClient.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatal(err)
	}
	return &Operator{client: cli, containers: map[string]ContainerInfo{}}
}

func (o *Operator) ReadConfig(path string) {
	bytes, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}

	var data parser.Config
	err = yaml.Unmarshal(bytes, &data)
	if err != nil {
		log.Fatal(err)
	}
	o.composeConfig = data
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
	var mutex sync.Mutex
	ctx := context.Background()
	eventsChan, errorsChan := o.client.Events(ctx, types.EventsOptions{})
	for {
		select {
		case event := <-eventsChan:
			if event.Type == TYPE_CONTAINER && (event.Action == ACTION_START || event.Action == ACTION_STOP) {
				go o.processEvent(ctx, event, &mutex)
			}
		case err := <-errorsChan:
			fmt.Println(err.Error())
		default:
			continue
		}
	}
}

func (o *Operator) processEvent(ctx context.Context, event events.Message, mutex *sync.Mutex) {
	if event.Action == ACTION_STOP {
		names := o.containers[event.ID].Names
		log.Printf("Container stopped ID: %s Names:%v", event.ID, names)
		mutex.Lock()
		delete(o.containers, event.ID)
		mutex.Unlock()
		return
	}

	container := o.getContainerInfo(ctx, event.ID)
	containerInfo := NewContainerInfo(&container)
	mutex.Lock()
	o.containers[containerInfo.Id] = *containerInfo
	mutex.Unlock()
	log.Println("Container started")
	log.Println(containerInfo)
	o.checkHash(ctx, containerInfo.Id, mutex)
	return
}

func (o *Operator) checkHash(ctx context.Context, containerId string, mutex *sync.Mutex) {
	container, _, err := o.client.ContainerInspectWithRaw(ctx, containerId, false)
	if err != nil {
		return
	}

	image, _, err := o.client.ImageInspectWithRaw(ctx, container.Image)
	if err != nil {
		return
	}

	imageName := getImageName(image.RepoTags[0])
	o.pullImage(ctx, imageName)
	o.updateImageAndContainer(ctx, imageName, containerId, mutex)
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

func (o *Operator) updateImageAndContainer(ctx context.Context, imageName string, containerId string, mutex *sync.Mutex) {
	images := o.getImages(ctx, imageName)
	if len(images) == 1 {
		log.Println("Container has latest image")
		return
	}

	var oldImageId string

	for _, image := range images {
		tag := getImageTag(image.RepoTags[0])
		if tag == "latest" {
			continue
		}
		oldImageId = image.ID
		break
	}

	containerConfig, containerName := o.getContainerConfig(imageName)

	duration := 5 * time.Second
	err := o.client.ContainerStop(ctx, containerId, &duration)
	if err != nil {
		return
	}

	err = o.client.ContainerRemove(ctx, containerId, types.ContainerRemoveOptions{})
	if err != nil {
		return
	}

	fmt.Println(oldImageId)
	_, err = o.client.ImageRemove(ctx, oldImageId, types.ImageRemoveOptions{})
	if err != nil {
		return
	}

	create, err := o.client.ContainerCreate(ctx, containerConfig, nil, nil, nil, containerName)
	if err != nil {
		return
	}

	if err := o.client.ContainerStart(ctx, create.ID, types.ContainerStartOptions{}); err != nil {
		panic(err)
	}

	mutex.Lock()
	newContainer := o.getContainerInfo(ctx, create.ID)
	newContainerInfo := NewContainerInfo(&newContainer)
	o.containers[create.ID] = *newContainerInfo
	mutex.Unlock()

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

func (o *Operator) getContainerConfig(imageName string) (*dockerContainer.Config, string) {
	for _, serviceConfig := range o.composeConfig.Services {
		if strings.HasPrefix(serviceConfig.Image, imageName) {
			containerConfig := &dockerContainer.Config{}
			containerConfig.Image = serviceConfig.Image
			portSet := nat.PortSet{}
			for _, configPort := range serviceConfig.Ports {
				port := nat.Port(configPort)
				portSet[port] = struct{}{}
			}
			containerConfig.ExposedPorts = portSet
			containerConfig.Env = serviceConfig.Environment
			containerConfig.Cmd = strings.Split(serviceConfig.Command, " ")
			volumesMap := make(map[string]struct{})
			for _, configVolume := range serviceConfig.Volumes {
				volumesMap[configVolume] = struct{}{}
			}
			containerConfig.Volumes = volumesMap
			return containerConfig, serviceConfig.ContainerName
		}
	}
	return nil, ""
}

func (o *Operator) getContainerInfo(ctx context.Context, containerId string) types.Container {

	filters := dockerFilters.NewArgs()
	filters.Add("id", containerId)

	containers, err := o.client.ContainerList(ctx, types.ContainerListOptions{
		Filters: filters,
	})
	if err != nil {
		log.Fatal("Error")
	}
	return containers[0]
}
