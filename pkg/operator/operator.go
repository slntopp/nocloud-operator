package operator

import (
	"context"
	"fmt"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"github.com/gorobot-nz/docker-operator/pkg/dns"
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
	dnsWrap    *dns.DnsWrap
}

func NewOperator() *Operator {
	cli, err := dockerClient.NewClientWithOpts(dockerClient.FromEnv, dockerClient.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatal(err)
	}

	bytes, err := ioutil.ReadFile("operator-config.yaml")
	if err != nil {
		log.Fatal(err)
	}

	var data OperatorConfig
	err = yaml.Unmarshal(bytes, &data)
	if err != nil {
		log.Fatal(err)
	}

	return &Operator{client: cli, containers: map[string]ContainerInfo{}, config: data}
}

func (o *Operator) ConfigureDns() error {
	ctx := context.Background()
	containersList, err := o.client.NetworkList(ctx, types.NetworkListOptions{})
	if err != nil {
		return err
	}

	dnsCheck, dnsMgmtCheck := false, false
	dnsIp, dnsMgmtIp, dnsNetworkName := "", "", ""

	for _, container := range containersList {
		if _, serverLabelOk := container.Labels[dns.ServerLabel]; serverLabelOk {
			dnsIp, err = o.getIpInNetwork(ctx, container.ID, container.Labels[dns.NetworkLabel])
			if err != nil {
				return err
			}
			dnsNetworkName = container.Labels[dns.NetworkLabel]
			dnsCheck = true
		} else if _, apiLabelOk := container.Labels[dns.ApiLabel]; apiLabelOk {
			dnsMgmtIp, err = o.getIpInNetwork(ctx, container.ID, container.Labels[dns.NetworkLabel])
			if err != nil {
				return err
			}
			dnsMgmtCheck = true
		}

		if dnsCheck && dnsMgmtCheck {
			o.dnsWrap = dns.NewDnsWrap(dnsNetworkName, dnsIp, dnsMgmtIp)
			return nil
		}
	}
	return nil
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
			for _, container := range o.containers {
				go o.CheckHash(ctx, container.Id)
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
	return
}

func (o *Operator) CheckHash(ctx context.Context, containerId string) {
	container, _, err := o.client.ContainerInspectWithRaw(ctx, containerId, false)
	if err != nil {
		return
	}

	endpointsConfig := getLinksAndAliases(container.NetworkSettings.Networks)
	labels := container.Config.Labels

	if _, ok := container.Config.Labels[dns.UpdateLabel]; ok {
		image, _, err := o.client.ImageInspectWithRaw(ctx, container.Image)
		if err != nil {
			return
		}

		o.pullImage(ctx, image.RepoTags[0])
		o.updateImageAndContainer(ctx, image.RepoTags[0], image.ID, containerId, container.HostConfig, labels, endpointsConfig)
	}
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

func (o *Operator) updateImageAndContainer(ctx context.Context, imageName string, imageId string, containerId string, hostCfg *dockerContainer.HostConfig, labels map[string]string, endpointsCfg *EndpointsConfig) {
	image := o.getImage(ctx, imageName)
	if image.ID == imageId {
		log.Println("Container is up to date")
		return
	}
	labels["com.docker.compose.image"] = image.ID

	err := o.removeOldImageAndContainer(ctx, containerId, imageId)
	if err != nil {
		log.Fatalf(err.Error())
	}

	err = o.createNewContainer(ctx, imageName, hostCfg, &labels, endpointsCfg)
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

func (o *Operator) getContainerComposeConfig(imageName string) (*dockerContainer.Config, *map[string]struct{}, string) {
	composeConfig := readComposeConfig("docker-compose.yml")

	for _, serviceConfig := range composeConfig.Services {
		if strings.HasSuffix(serviceConfig.Image, imageName) {
			containerConfig := &dockerContainer.Config{}
			containerConfig.Image = serviceConfig.Image
			containerConfig.Env = convertEnvMapToArray(getEnvValues(serviceConfig.Environment))
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

			return containerConfig, &networks, serviceConfig.ContainerName
		}
	}
	return nil, nil, ""
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

func (o *Operator) createNewContainer(ctx context.Context, imageName string, hostCfg *dockerContainer.HostConfig, labels *map[string]string, e *EndpointsConfig) error {
	containerConfig, networksNames, containerName := o.getContainerComposeConfig(imageName)
	containerConfig.Labels = *labels

	if _, ok := containerConfig.Labels[dns.DnsRequiredLabel]; ok {
		hostCfg.DNS = []string{o.dnsWrap.DnsIp}
		hostCfg.DNSSearch = []string{}
		hostCfg.DNSOptions = []string{}
	}

	create, err := o.client.ContainerCreate(ctx, containerConfig, hostCfg, nil, nil, containerName)
	if err != nil {
		return err
	}

	if err := o.client.ContainerStart(ctx, create.ID, types.ContainerStartOptions{}); err != nil {
		return err
	}

	err = o.connectNetworks(ctx, create.ID, networksNames, e)
	if err != nil {
		return err
	}

	return nil
}

func (o *Operator) getIpInNetwork(ctx context.Context, containerId string, networkName string) (string, error) {
	containerInfo, _, err := o.client.ContainerInspectWithRaw(ctx, containerId, false)
	if err != nil {
		return "", err
	}

	return containerInfo.NetworkSettings.Networks[o.config.ComposePrefix+networkName].IPAddress, nil
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

	o.configureDnsMgmtRecords(ctx, containerId)

	return nil
}

func (o *Operator) configureDnsMgmtRecords(ctx context.Context, id string) {
	container, _, err := o.client.ContainerInspectWithRaw(ctx, id, false)
	if err != nil {
		return
	}
	labels := container.Config.Labels
	if zoneLabelValue, ok := labels[dns.ZoneLabel]; ok {
		ip, err := o.getIpInNetwork(ctx, id, labels[dns.NetworkLabel])
		if err != nil {
			return
		}
		locations := make(map[string]string)
		for key, value := range labels {
			if strings.HasPrefix(key, dns.KeyLabel) {
				splitedKey := strings.Split(key, ".")
				locations[value] = splitedKey[len(splitedKey)-1]
			}
		}
		err = o.dnsWrap.Get(ctx, zoneLabelValue, ip, locations)
		if err != nil {
			log.Fatal(err.Error())
		}
	}
}

func readComposeConfig(path string) Config {
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

func getNecessaryNetworks(list *[]types.NetworkResource, endpointsNames map[string]struct{}) []string {
	networkIds := make([]string, 0)
	for _, item := range *list {
		if _, ok := endpointsNames[item.Name]; ok {
			networkIds = append(networkIds, item.ID)
		}
	}
	return networkIds
}

func convertEnvMapToArray(envMap map[string]string) []string {
	result := make([]string, 0)

	for key, value := range envMap {
		result = append(result, fmt.Sprintf("%s=%s", key, getEnvValue(value)))
	}
	return result
}

func getEnvValues(environment map[string]string) map[string]string {
	result := make(map[string]string, 0)
	for key, value := range environment {
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

func getLinksAndAliases(networks map[string]*network.EndpointSettings) *EndpointsConfig {
	for _, value := range networks {
		return &EndpointsConfig{
			Links:   value.Links,
			Aliases: value.Aliases[:len(value.Aliases)-1],
		}
	}
	return nil
}
