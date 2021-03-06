package operator

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"github.com/slntopp/nocloud-operator/pkg/dns"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/docker/docker/api/types"
	dockerContainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	dockerFilters "github.com/docker/docker/api/types/filters"
	dockerClient "github.com/docker/docker/client"
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

	log *zap.Logger
}

func NewOperator(logger *zap.Logger) *Operator {
	log := logger.Named("Operator")
	cli, err := dockerClient.NewClientWithOpts(dockerClient.FromEnv, dockerClient.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatal("Failed creating Client", zap.Error(err))
	}

	bytes, err := ioutil.ReadFile("operator-config.yml")
	if err != nil {
		log.Fatal("Failed reading operator config", zap.Error(err))
	}

	var data OperatorConfig
	err = yaml.Unmarshal(bytes, &data)
	if err != nil {
		log.Fatal("Failed Unmarshal operator config", zap.Error(err))
	}

	return &Operator{client: cli, containers: map[string]ContainerInfo{}, config: data, log: log}
}

func (o *Operator) ConfigureDns() error {
	log := o.log.Named("configure_dns")
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
			o.dnsWrap = dns.NewDnsWrap(log, dnsNetworkName, dnsIp, dnsMgmtIp)
			return nil
		}
	}
	return nil
}

func (o *Operator) Ps() map[string]ContainerInfo {
	log := o.log.Named("ps")
	ctx := context.Background()
	containers, err := o.client.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		log.Fatal("Error listing Containers", zap.Error(err))
	}

	for _, container := range containers {
		o.containers[container.ID] = *NewContainerInfo(&container)
		go o.configureDnsMgmtRecords(ctx, container.ID)
	}
	return o.containers
}

func (o *Operator) ObserveContainers() {
	log := o.log.Named("Observer")

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
			log.Info("Log after", zap.Int("seconds", o.config.Duration))
			for _, container := range o.containers {
				go o.checkHash(ctx, container.Id)
			}
		case err := <-errorsChan:
			log.Error("Error in channel", zap.Error(err))
		default:
			continue
		}
	}
}

func (o *Operator) processEvent(ctx context.Context, event events.Message, mutex *sync.Mutex) {
	log := o.log.Named("process_event")

	if event.Action == ActionStop {
		names := o.containers[event.ID].Names
		log.Info("Container stopped", zap.String("id", event.ID), zap.Strings("names", names))
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

	log.Info("Container started", zap.Any("info", containerInfo))
}

func (o *Operator) checkHash(ctx context.Context, containerId string) {
	log := o.log.Named("check_hash")
	container, _, err := o.client.ContainerInspectWithRaw(ctx, containerId, false)
	if err != nil {
		return
	}

	labels := container.Config.Labels

	if _, ok := container.Config.Labels[dns.UpdateLabel]; ok {
		image, _, err := o.client.ImageInspectWithRaw(ctx, container.Image)
		if err != nil {
			return
		}

		if len(image.RepoTags) == 0 {
			log.Error("Something wrong with the tags of your image: none given")
			return
		}

		endpointsConfig := getLinksAndAliases(container.NetworkSettings.Networks, container.ID)
		log.Info("Pulling image", zap.String("tag", image.RepoTags[0]))
		o.pullImage(ctx, image.RepoTags[0])

		log.Info("Updating image and Container", zap.String("tag", image.RepoTags[0]), zap.String("container", container.Name))
		o.updateImageAndContainer(ctx, image.RepoTags[0], image.ID, containerId, container.Name, container.HostConfig, labels, endpointsConfig)
	}
}

func (o *Operator) pullImage(ctx context.Context, imageName string) {
	log := o.log.Named("pull_image")

	out, err := o.client.ImagePull(ctx, imageName, types.ImagePullOptions{})
	if err != nil {
		log.Error("Error while pulling image", zap.String("image", imageName), zap.Error(err))
		return
	}

	defer func(out io.ReadCloser) {
		err := out.Close()
		if err != nil {
			log.Warn("Something's wrong while closing contaner pull err", zap.Error(err))
		}
	}(out)

	_, err = io.Copy(os.Stdout, out)
	if err != nil {
		log.Error("Wrong stream", zap.Error(err))
		return
	}
}

func (o *Operator) updateImageAndContainer(ctx context.Context, imageName string, imageId string, containerId string, containerName string, hostCfg *dockerContainer.HostConfig, labels map[string]string, endpointsCfg *EndpointsConfig) {
	log := o.log.Named("update_image_and_container")

	image := o.getImage(ctx, imageName)
	if image.ID == imageId {
		log.Info("Container is up to date")
		return
	}
	labels["com.docker.compose.image"] = image.ID

	err := o.removeOldImageAndContainer(ctx, containerId, imageId)
	if err != nil {
		log.Error("Error while deleting old image and container", zap.Error(err))
	}

	err = o.createNewContainer(ctx, imageName, hostCfg, containerName, &labels, endpointsCfg)
	if err != nil {
		log.Error("Error while creating new container", zap.Error(err))
	}
}

func (o *Operator) getImage(ctx context.Context, imageName string) types.ImageSummary {
	log := o.log.Named("get_image")

	filters := dockerFilters.NewArgs()
	filters.Add("reference", imageName)

	images, err := o.client.ImageList(ctx, types.ImageListOptions{Filters: filters})
	if err != nil {
		log.Error("Error while getting image", zap.Error(err))
	}
	return images[0]
}

func (o *Operator) getContainer(ctx context.Context, containerId string) types.Container {
	log := o.log.Named("get_container")

	filters := dockerFilters.NewArgs()
	filters.Add("id", containerId)

	containers, err := o.client.ContainerList(ctx, types.ContainerListOptions{
		Filters: filters,
	})
	if err != nil {
		log.Error("Error while getting container", zap.Error(err))
	}
	return containers[0]
}

func (o *Operator) getContainerComposeConfig(imageName string) (*dockerContainer.Config, *map[string]struct{}) {
	log := o.log.Named("get_container_compose_config")

	composeConfig := readComposeConfig("docker-compose.yml", log)

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

			return containerConfig, &networks
		}
	}
	return nil, nil
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

func (o *Operator) createNewContainer(ctx context.Context, imageName string, hostCfg *dockerContainer.HostConfig, containerName string, labels *map[string]string, e *EndpointsConfig) error {
	containerConfig, networksNames := o.getContainerComposeConfig(imageName)
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
	log := o.log.Named("configure_dns_mgmt_records")

	container, _, err := o.client.ContainerInspectWithRaw(ctx, id, false)
	if err != nil {
		return
	}
	labels := container.Config.Labels
	if zoneLabelValue, ok := labels[dns.ZoneLabel]; ok {
		ip, _ := o.getIpInNetwork(ctx, id, labels[dns.NetworkLabel]) // TODO: handle error
		aValue := labels[dns.ALabel]
		err = o.dnsWrap.Get(ctx, zoneLabelValue, ip, aValue)
		if err != nil {
			log.Error("DNS Error", zap.Error(err))
		}
	}
}

func readComposeConfig(path string, log *zap.Logger) Config {
	bytes, err := ioutil.ReadFile(path)
	if err != nil {
		log.Error("Error reading Compose file", zap.Error(err))
	}

	var data Config
	err = yaml.Unmarshal(bytes, &data)
	if err != nil {
		log.Error("Error Unmarshal Compose file", zap.Error(err))
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

func getLinksAndAliases(networks map[string]*network.EndpointSettings, id string) *EndpointsConfig {
	for _, value := range networks {
		var links = value.Links
		var aliases = value.Aliases

		if len(aliases) != 0 {
			for i, value := range aliases {
				if strings.HasPrefix(id, value) {
					aliases = append(aliases[:i], aliases[i+1:]...)
					break
				}
			}
		}

		return &EndpointsConfig{
			Links:   links,
			Aliases: aliases,
		}
	}
	return nil
}
