package operator

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"google.golang.org/grpc/metadata"
	"io"
	"os"
	"reflect"
	"sort"
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
	dockerFilters "github.com/docker/docker/api/types/filters"
	dockerClient "github.com/docker/docker/client"
)

func encodeToBase64(v interface{}) (string, error) {
	var buf bytes.Buffer
	encoder := base64.NewEncoder(base64.StdEncoding, &buf)
	err := json.NewEncoder(encoder).Encode(v)
	if err != nil {
		return "", err
	}
	encoder.Close()
	return buf.String(), nil
}

type Operator struct {
	client     *dockerClient.Client
	containers map[string]ContainerInfo
	config     OperatorConfig
	dnsWrap    *dns.DnsWrap
	mutex      sync.Mutex
	//traefikClient *traefik.TraefikClient
	//traefikId     string
	token        string
	dockerTokens []string
	defaultDns   []string

	notRunningContainers []string
	networkNames         map[string]*map[string]struct{}
	endpoints            map[string]*EndpointsConfig

	drivers []string

	log *zap.Logger
}

func NewOperator(logger *zap.Logger, token string) *Operator {
	log := logger.Named("Operator")
	cli, err := dockerClient.NewClientWithOpts(dockerClient.FromEnv, dockerClient.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatal("Failed creating Client", zap.Error(err))
	}

	bytes, err := os.ReadFile("./operator-config.yml")
	if err != nil {
		log.Fatal("Failed reading operator config", zap.Error(err))
	}

	var data OperatorConfig
	err = yaml.Unmarshal(bytes, &data)
	if err != nil {
		log.Fatal("Failed Unmarshal operator config", zap.Error(err))
	}

	dockerTokens := []string{""}

	for _, registry := range data.DockerRegistries {

		if registry.Username != "" && registry.Password != "" && registry.ServerAddress != "" {
			_, err = cli.RegistryLogin(context.Background(), types.AuthConfig{
				Username:      registry.Username,
				Password:      registry.Password,
				ServerAddress: registry.ServerAddress,
			})

			if err != nil {
				log.Fatal("No registry", zap.Error(err))
			}

			var dockerCreds = Registries{
				Username:      registry.Username,
				Password:      registry.Password,
				ServerAddress: registry.ServerAddress,
			}

			dockerToken, _ := encodeToBase64(dockerCreds)
			dockerTokens = append(dockerTokens, dockerToken)
		}
	}
	operator := &Operator{
		client:       cli,
		containers:   map[string]ContainerInfo{},
		config:       data,
		log:          log,
		token:        token,
		defaultDns:   data.Dns,
		drivers:      []string{},
		dockerTokens: dockerTokens,
	}

	return operator
}

func (o *Operator) Wait() {
	wait := true
	log := o.log.Named("wait")
	config := readComposeConfig("./docker-compose.yml", log)
	for wait {
		list, err := o.client.ContainerList(context.Background(), types.ContainerListOptions{})
		if err != nil {
			return
		}

		log.Info("docker-compose", zap.Int("count", len(config.Services)))
		log.Info("list", zap.Int("count", len(list)))
		if len(config.Services) == len(list) {
			wait = false
		}
		log.Info("Waiting for all containers start")
		time.Sleep(5 * time.Second)
	}
}

func (o *Operator) ConfigureDns() error {
	log := o.log.Named("configure_dns")
	ctx := context.Background()
	containersList, err := o.client.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		return err
	}

	dnsIp, dnsMgmtHost, dnsNetworkName := "", "", ""

	dnsCheck, dnsMgmtCheck := false, false

	for _, container := range containersList {
		if _, ok := container.Labels[dns.ServerLabel]; ok {
			dnsIp, err = o.getIpInNetwork(ctx, container.ID, container.Labels[dns.NetworkLabel])
			if err != nil {
				return err
			}
			dnsNetworkName = container.Labels[dns.NetworkLabel]
			dnsCheck = true
		}

		if _, ok := container.Labels[dns.ApiLabel]; ok {
			containerInspect, _, err := o.client.ContainerInspectWithRaw(ctx, container.ID, false)
			if err != nil {
				return err
			}
			dnsMgmtHost = containerInspect.Config.Hostname
			dnsMgmtCheck = true
		}

		if dnsCheck && dnsMgmtCheck {
			o.dnsWrap = dns.NewDnsWrap(log, dnsNetworkName, dnsIp, dnsMgmtHost)
			return nil
		}

	}
	return errors.New("no dns server")
}

func (o *Operator) SetDnsIpToContainers() error {
	log := o.log.Named("set_dns_ip")
	ctx := context.Background()
	containersList, err := o.client.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		return err
	}

	for _, container := range containersList {
		if _, ok := container.Labels[dns.DnsRequiredLabel]; ok {
			err := o.recreateContainer(ctx, container.ID)
			if err != nil {
				log.Fatal("Fail to set DNS ip", zap.String("err", err.Error()))
			}
		}
	}
	return nil
}

func (o *Operator) recreateContainer(ctx context.Context, id string) error {
	log := o.log.Named("restart_container_with_dns")
	container, _, err := o.client.ContainerInspectWithRaw(ctx, id, false)
	if err != nil {
		return err
	}

	labels := container.Config.Labels

	image, _, err := o.client.ImageInspectWithRaw(ctx, container.Image)
	if err != nil {
		return err
	}

	if len(image.RepoTags) == 0 {
		log.Error("Something wrong with the tags of your image: none given")
		return err
	}

	endpointsConfig := getLinksAndAliases(container.NetworkSettings.Networks, container.ID)

	options := dockerContainer.StopOptions{
		Signal:  "SIGKILL",
		Timeout: nil,
	}
	err = o.client.ContainerStop(ctx, id, options)
	if err != nil {
		return err
	}

	err = o.client.ContainerRemove(ctx, id, types.ContainerRemoveOptions{})
	if err != nil {
		return err
	}

	names := o.containers[id].Names
	log.Info("Container stopped", zap.String("id", id), zap.Strings("names", names))
	delete(o.containers, id)

	err = o.createNewContainer(ctx, image.RepoTags[0], container.HostConfig, container.Name, &labels, endpointsConfig)
	if err != nil {
		return err
	}
	return nil
}

/*
func (o *Operator) ConnectToTraefik(host string) error {
	log := o.log.Named("connection_to_traefik")
	o.traefikClient = traefik.NewTraefikClient(host)
	err := o.traefikClient.Ping()
	if err != nil {
		log.Error("Error", zap.String("err", err.Error()))
		return err
	}

	list, _ := o.client.ContainerList(context.Background(), types.ContainerListOptions{})

	for _, item := range list {
		if item.Image == "traefik:latest" {
			for key := range item.NetworkSettings.Networks {
				if strings.HasSuffix(key, "proxy") {
					o.traefikId = item.ID
					log.Info("ID " + item.ID)
					break
				}
			}
			break
		}
	}

	return nil
}
*/

/*
func (o *Operator) CheckTraefik(ctx context.Context) {
	log := o.log.Named("check_traefik")
	traefikServices := o.traefikClient.GetCountOfServices(o.log.Named("traefik_containers"))
	configServices := readComposeConfig("./docker-compose.yml", log).Services
	filteredConfigServices := 0

	for _, value := range configServices {
		for _, label := range value.Labels {
			if strings.HasPrefix(label, "traefik.enable") {
				log.Info("From compose", zap.String("name", value.ContainerName))
				filteredConfigServices += 1
				break
			}
		}
	}

	log.Info("Services from traefik", zap.Int("count", traefikServices))
	log.Info("Services from config", zap.Int("count", filteredConfigServices))

	if traefikServices != filteredConfigServices {
		o.RestartTraefik(ctx, o.traefikId)
	}
}
*/

/*
func (o *Operator) RestartTraefik(ctx context.Context, id string) {
	log := o.log.Named("Restart traefik")
	log.Info("Restart")
	duration := 10 * time.Second
	err := o.client.ContainerRestart(ctx, id, &duration)
	if err != nil {
		log.Error("FUCK")
		return
	}
}
*/

func (o *Operator) Ps() map[string]ContainerInfo {
	log := o.log.Named("ps")

	for key := range o.containers {
		delete(o.containers, key)
	}

	ctx := context.Background()
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+o.token)
	containers, err := o.client.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		log.Fatal("Error listing Containers", zap.Error(err))
	}

	for _, container := range containers {
		o.containers[container.ID] = *NewContainerInfo(&container)
		o.configureDnsMgmtRecords(ctx, container.ID)
	}
	return o.containers
}

func (o *Operator) ObserveContainers() {
	log := o.log.Named("Observer")

	ctx := context.Background()
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+o.token)
	_, errorsChan := o.client.Events(ctx, types.EventsOptions{})

	ticker := time.NewTicker(time.Duration(o.config.Duration) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			var wg sync.WaitGroup
			log.Info("count of containers", zap.Int("count", len(o.containers)))
			wg.Add(len(o.containers))
			o.Ps()
			for _, container := range o.containers {
				go o.checkHash(ctx, container.Id, container.Image, &wg)
			}
			wg.Wait()
			//o.CheckTraefik(ctx)
			o.checkDrivers(ctx)
			log.Info("Another cycle")
		case err := <-errorsChan:
			log.Error("Error in channel", zap.Error(err))
		default:
			continue
		}
	}
}

func (o *Operator) checkDrivers(ctx context.Context) {
	log := o.log.Named("check_drivers")
	var drivers = make([]string, 0)

	containersList, err := o.client.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		log.Error("Error to get conainers", zap.String("err", err.Error()))
		return
	}

	driversPort := os.Getenv("DRIVER_PORT")
	if driversPort == "" {
		driversPort = "8080"
	}

	for _, container := range containersList {
		if _, ok := container.Labels[dns.DriverLabel]; ok {
			containerInspect, _, err := o.client.ContainerInspectWithRaw(ctx, container.ID, false)
			if err != nil {
				log.Error("Error to get conainers", zap.String("err", err.Error()))
				continue
			}

			drivers = append(drivers, fmt.Sprintf("%s:%s", containerInspect.Config.Hostname, driversPort))
			continue
		}
	}

	sort.Strings(o.drivers)
	sort.Strings(drivers)

	if !reflect.DeepEqual(o.drivers, drivers) {
		o.drivers = drivers

		for _, container := range containersList {
			if _, ok := container.Labels[dns.WithDriversLabel]; ok {
				err := o.recreateContainer(ctx, container.ID)
				if err != nil {
					log.Error("Recreating container", zap.String("err", err.Error()))
					return
				}
			}
		}
	}

}

func (o *Operator) checkHash(ctx context.Context, containerId, containerName string, wg *sync.WaitGroup) {
	log := o.log.Named("check_hash")
	container, _, err := o.client.ContainerInspectWithRaw(ctx, containerId, false)
	if err != nil {
		return
	}

	labels := container.Config.Labels

	defer wg.Done()

	if _, ok := container.Config.Labels[dns.UpdateLabel]; ok {
		image, _, err := o.client.ImageInspectWithRaw(ctx, container.Image)
		if err != nil {
			log.Error("Image inspect with raw", zap.String("err", err.Error()))
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
	log.Info("Wg Done", zap.String("id", containerId), zap.String("name", containerName))
}

func (o *Operator) pullImage(ctx context.Context, imageName string) {
	log := o.log.Named("pull_image")

	for _, token := range o.dockerTokens {
		out, err := o.client.ImagePull(ctx, imageName, types.ImagePullOptions{
			RegistryAuth: token,
		})
		if err != nil {
			log.Error("Error while pulling image", zap.String("image", imageName), zap.Error(err))
			continue
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
}

func (o *Operator) updateImageAndContainer(ctx context.Context, imageName string, imageId string, containerId string, containerName string, hostCfg *dockerContainer.HostConfig, labels map[string]string, endpointsCfg *EndpointsConfig) {
	log := o.log.Named("update_image_and_container")

	image := o.getImage(ctx, imageName)
	if image.ID == imageId {
		log.Info("Container is up to date")
		return
	}
	labels["com.docker.compose.image"] = image.ID

	o.mutex.Lock()

	err := o.removeOldImageAndContainer(ctx, containerId, imageId)
	if err != nil {
		log.Error("Error while deleting old image and container", zap.Error(err))
	}

	err = o.createNewContainer(ctx, imageName, hostCfg, containerName, &labels, endpointsCfg)
	if err != nil {
		log.Error("Error while creating new container", zap.Error(err))
	}
	o.mutex.Unlock()
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

	composeConfig := readComposeConfig("./docker-compose.yml", log)

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
	options := dockerContainer.StopOptions{
		Signal:  "SIGKILL",
		Timeout: nil,
	}
	err := o.client.ContainerStop(ctx, containerId, options)
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

	log := o.log.Named("process_event")
	names := o.containers[containerId].Names
	log.Info("Container stopped", zap.String("id", containerId), zap.Strings("names", names))
	delete(o.containers, containerId)

	return nil
}

func (o *Operator) createNewContainer(ctx context.Context, imageName string, hostCfg *dockerContainer.HostConfig, containerName string, labels *map[string]string, e *EndpointsConfig) error {

	containerConfig, networksNames := o.getContainerComposeConfig(imageName)
	containerConfig.Labels = *labels

	if _, ok := containerConfig.Labels[dns.DnsRequiredLabel]; ok {
		hostCfg.DNS = []string{o.dnsWrap.DnsIp}
		hostCfg.DNS = append(hostCfg.DNS, o.defaultDns...)
		hostCfg.DNSSearch = []string{}
		hostCfg.DNSOptions = []string{}
	}

	if _, ok := containerConfig.Labels[dns.WithDriversLabel]; ok {
		stringDrivers := strings.Join(o.drivers, " ")
		containerConfig.Env = append(containerConfig.Env, fmt.Sprintf("DRIVERS=%s", stringDrivers))
	}

	create, err := o.client.ContainerCreate(ctx, containerConfig, hostCfg, nil, nil, containerName)
	if err != nil {
		return err
	}

	if err := o.client.ContainerStart(ctx, create.ID, types.ContainerStartOptions{}); err != nil {
		o.notRunningContainers = append(o.notRunningContainers, create.ID)
		o.networkNames[create.ID] = networksNames
		o.endpoints[create.ID] = e
		return err
	}

	container := o.getContainer(ctx, create.ID)
	containerInfo := NewContainerInfo(&container)

	o.containers[containerInfo.Id] = *containerInfo

	err = o.connectNetworks(ctx, create.ID, networksNames, e)
	if err != nil {
		return err
	}

	return nil
}

func (o *Operator) getIpInNetwork(ctx context.Context, containerId string, networkName string) (string, error) {
	log := o.log.Named(fmt.Sprintf("Attempt to get ip. Container: %s, Network: %s", containerId, networkName))
	containerInfo, _, err := o.client.ContainerInspectWithRaw(ctx, containerId, false)
	if err != nil {
		log.Error("Something wrong with docker", zap.String("err", err.Error()))
		return "", err
	}

	if info, ok := containerInfo.NetworkSettings.Networks[o.config.ComposePrefix+networkName]; !ok {
		log.Error("No such network")
		return "", errors.New("no such network")
	} else {
		return info.IPAddress, nil
	}
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

		log.Info("ContainerId", zap.String("id", id))
		log.Info("Container got zone label")

		net := labels[dns.NetworkLabel]

		ip, err := o.getIpInNetwork(ctx, id, net) // TODO: handle error

		if err != nil {
			log.Error("Fail to get ip in zone", zap.String("container id", id))
			return
		}

		log.Info("Ip in net", zap.String("ip", ip), zap.String("net", net))

		aValue := labels[dns.ALabel]
		err = o.dnsWrap.Get(ctx, zoneLabelValue, ip, aValue)
		if err != nil {
			log.Error("DNS Error", zap.Error(err))
		}
	}
}

func readComposeConfig(path string, log *zap.Logger) Config {
	bytes, err := os.ReadFile(path)
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
