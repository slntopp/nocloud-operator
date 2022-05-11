package operator

import (
	"fmt"
	"github.com/docker/docker/api/types"
)

type ContainerInfo struct {
	ShortId string
	Id      string
	Names   []string
	Image   string
	Labels  map[string]string
}

func NewContainerInfo(container *types.Container) *ContainerInfo {
	shortId := container.ID[:6]
	return &ContainerInfo{Id: container.ID, ShortId: shortId, Image: container.Image, Labels: container.Labels, Names: container.Names}
}

func (c ContainerInfo) String() string {
	labels := ""
	for key, value := range c.Labels {
		labels += fmt.Sprintf("\tLKey: %s\t LValue: %s\n", key, value)
	}
	return fmt.Sprintf("Names:%v\nID:%v\nImage:%v\nLabels:\n%v", c.Names, c.ShortId, c.Image, labels)
}
