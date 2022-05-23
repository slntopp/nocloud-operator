package operator

import (
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

type CreateContainerConfig struct {
	Cfg        *container.Config
	HostCfg    *container.HostConfig
	NetworkCfg *network.NetworkingConfig
}

func NewCreateContainerConfig(cfg *container.Config, hostCfg *container.HostConfig, networkCfg *network.NetworkingConfig) *CreateContainerConfig {
	return &CreateContainerConfig{Cfg: cfg, HostCfg: hostCfg, NetworkCfg: networkCfg}
}
