package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/docker/docker/api/types/network"
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
	log "github.com/sirupsen/logrus"
)

const (
	TypeContainer = "container"
	ActionStart   = "start"
	ActionStop    = "stop"
)

type Operator struct {
	client     *dockerClient.Client
	containers map[string]ContainerInfo
	config     OperatorConfig
}

func NewOperator() *Operator {
	cli, err := dockerClient.NewClientWithOpts(dockerClient.FromEnv, dockerClient.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatal(err)
	}

	bytes, err := ioutil.ReadFile("operator-config.json")
	if err != nil {
		log.Fatal(err)
	}

	var data OperatorConfig
	err = json.Unmarshal(bytes, &data)
	if err != nil {
		log.Fatal(err)
	}

	return &Operator{client: cli, containers: map[string]ContainerInfo{}, config: data}
}

func (o *Operator) readComposeConfig(path string) Config {
	bytes, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}

	var data Config
	err = yaml.Unmarshal(bytes, &data)
	if err != nil {
		log.Fatal(err)
	}
	return data
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

	ticker := time.NewTicker(time.Duration(o.config.Duration) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case event := <-eventsChan:
			if event.Type == TypeContainer && (event.Action == ActionStart || event.Action == ActionStop) {
				go o.processEvent(ctx, event, &mutex)
			}
		case <-ticker.C:
			list, err := o.client.ContainerList(ctx, types.ContainerListOptions{})
			if err != nil {
				return
			}
			for _, container := range list {
				go o.checkHash(ctx, container.ID)
			}
		case err := <-errorsChan:
			fmt.Println(err.Error())
		default:
			continue
		}
	}
}

func (o *Operator) processEvent(ctx context.Context, event events.Message, mutex *sync.Mutex) {
	if event.Action == ActionStop {
		names := o.containers[event.ID].Names
		log.Printf("Container stopped ID: %s Names:%v", event.ID, names)
		mutex.Lock()
		delete(o.containers, event.ID)
		mutex.Unlock()
		return
	}

	container := o.getContainer(ctx, event.ID)
	containerInfo := NewContainerInfo(&container)
	mutex.Lock()
	o.containers[containerInfo.Id] = *containerInfo
	mutex.Unlock()
	log.Println("Container started")
	log.Println(containerInfo)
	o.checkHash(ctx, container.ID)
	return
}

func (o *Operator) checkHash(ctx context.Context, containerId string) {
	container, _, err := o.client.ContainerInspectWithRaw(ctx, containerId, false)
	if err != nil {
		return
	}

	image, _, err := o.client.ImageInspectWithRaw(ctx, container.Image)
	if err != nil {
		return
	}

	o.pullImage(ctx, image.RepoTags[0])
	o.updateImageAndContainer(ctx, image.RepoTags[0], image.ID, containerId, container.HostConfig)
}

func (o *Operator) pullImage(ctx context.Context, imageName string) {
	out, err := o.client.ImagePull(ctx, imageName, types.ImagePullOptions{})
	if err != nil {
		panic(err)
	}

	defer func(out io.ReadCloser) {
		err := out.Close()
		if err != nil {
			log.Fatal("Somethings wrong")
		}
	}(out)

	_, err = io.Copy(os.Stdout, out)
	if err != nil {
		log.Fatal("Wrong stream")
		return
	}
}

func (o *Operator) updateImageAndContainer(ctx context.Context, imageName string, imageId string, containerId string, hostCfg *dockerContainer.HostConfig) {
	image := o.getImage(ctx, imageName)
	if image.ID == imageId {
		log.Println("Container is up to date")
		return
	}

	err := o.removeOldImageAndContainer(ctx, containerId, imageId)
	if err != nil {
		log.Fatalf(err.Error())
	}

	err = o.createNewContainer(ctx, imageName, hostCfg)
	if err != nil {
		log.Fatalf(err.Error())
	}
}

func (o *Operator) getImage(ctx context.Context, imageName string) types.ImageSummary {
	filters := dockerFilters.NewArgs()
	filters.Add("reference", imageName)
	images, err := o.client.ImageList(ctx, types.ImageListOptions{Filters: filters})
	if err != nil {
		log.Fatal(err)
	}
	return images[0]
}

func (o *Operator) getContainerComposeConfig(imageName string) (*dockerContainer.Config, *map[string]struct{}, string, *EndpointsConfig) {
	composeConfig := o.readComposeConfig("docker-compose.yml")

	for _, serviceConfig := range composeConfig.Services {
		if strings.HasSuffix(serviceConfig.Image, imageName) {
			containerConfig := &dockerContainer.Config{}
			containerConfig.Image = serviceConfig.Image
			containerConfig.Env = convertEnvMapToString(serviceConfig.Environment)
			if serviceConfig.Command != "" {
				containerConfig.Cmd = strings.Split(serviceConfig.Command, " ")
			}
			portSet := nat.PortSet{}
			for _, configPort := range serviceConfig.Ports {
				port := nat.Port(configPort)
				portSet[port] = struct{}{}
			}
			containerConfig.ExposedPorts = portSet
			volumesMap := make(map[string]struct{})
			for _, configVolume := range serviceConfig.Volumes {
				volumesMap[configVolume] = struct{}{}
			}
			containerConfig.Volumes = volumesMap
			networks := make(map[string]struct{}, 0)
			for _, value := range serviceConfig.Networks {
				networks[o.config.ComposePrefix+value] = struct{}{}
			}
			containerConfig.Labels = getLabelsWithEnv(serviceConfig.Labels)

			var endpointsConfig EndpointsConfig
			endpointsConfig.Links = serviceConfig.Links
			endpointsConfig.Aliases = []string{serviceConfig.ContainerName}
			return containerConfig, &networks, serviceConfig.ContainerName, &endpointsConfig
		}
	}
	return nil, nil, "", nil
}

func (o *Operator) getContainer(ctx context.Context, containerId string) types.Container {
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

func (o *Operator) removeOldImageAndContainer(ctx context.Context, containerId, imageId string) error {
	duration := 5 * time.Second
	err := o.client.ContainerStop(ctx, containerId, &duration)
	if err != nil {
		return err
	}

	err = o.client.ContainerRemove(ctx, containerId, types.ContainerRemoveOptions{})
	if err != nil {
		return err
	}

	_, err = o.client.ImageRemove(ctx, imageId, types.ImageRemoveOptions{})
	if err != nil {
		return err
	}
	return nil
}

func (o *Operator) createNewContainer(ctx context.Context, imageName string, hostCfg *dockerContainer.HostConfig) error {
	containerConfig, networksNames, containerName, endpointsConfig := o.getContainerComposeConfig(imageName)

	create, err := o.client.ContainerCreate(ctx, containerConfig, hostCfg, nil, nil, containerName)
	if err != nil {
		return err
	}

	if err := o.client.ContainerStart(ctx, create.ID, types.ContainerStartOptions{}); err != nil {
		return err
	}

	err = o.connectNetworks(ctx, create.ID, networksNames, endpointsConfig)
	if err != nil {
		return err
	}

	return nil
}

func (o *Operator) connectNetworks(ctx context.Context, containerId string, endpointsNames *map[string]struct{}, config *EndpointsConfig) error {
	networksList, err := o.client.NetworkList(ctx, types.NetworkListOptions{})
	if err != nil {
		return err
	}
	networkIds := getNecessaryNetworks(&networksList, *endpointsNames)

	inspectedContainer, err := o.client.ContainerInspect(ctx, containerId)
	if err != nil {
		return err
	}

	for _, value := range inspectedContainer.NetworkSettings.Networks {
		o.client.NetworkDisconnect(ctx, value.NetworkID, containerId, true)
	}

	for _, id := range networkIds {
		o.client.NetworkConnect(ctx, id, containerId, &network.EndpointSettings{
			Links:   config.Links,
			Aliases: config.Aliases,
		})
	}

	return nil
}

func getNecessaryNetworks(list *[]types.NetworkResource, endpointsNames map[string]struct{}) []string {
	networkIds := make([]string, 0)
	for _, item := range *list {
		if _, ok := endpointsNames[item.Name]; ok {
			networkIds = append(networkIds, item.ID)
		}
	}
	return networkIds
}

func convertEnvMapToString(envMap map[string]string) []string {
	result := make([]string, 0)

	for key, value := range envMap {
		result = append(result, fmt.Sprintf("%s=%s", key, getEnvValue(value)))
	}
	return result
}

func getLabelsWithEnv(labels map[string]string) map[string]string {
	result := make(map[string]string, 0)
	for key, value := range labels {
		result[key] = getEnvValue(value)
	}
	return result
}

func getEnvValue(value string) string {
	var result []rune
	valueSymbols := []rune(value)

	for i := 0; i < len(valueSymbols); i++ {
		if valueSymbols[i] == '$' {
			startIndex := i + 2
			for valueSymbols[i] != '}' {
				i++
			}
			endIndex := i
			result = append(result, []rune(os.Getenv(string(valueSymbols[startIndex:endIndex])))...)
			continue
		}
		result = append(result, valueSymbols[i])
	}

	return string(result)
}
