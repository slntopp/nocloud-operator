package operator

import (
	"fmt"
	"github.com/docker/docker/api/types"
)

type ContainerInfo struct {
	ShortId string
	Image   string
	Labels  map[string]string
}

func NewContainerInfo(container *types.Container) *ContainerInfo {
	shortId := container.ID[:6]
	return &ContainerInfo{ShortId: shortId, Image: container.Image, Labels: container.Labels}
}

func (c ContainerInfo) String() string {
	labels := ""
	for key, value := range c.Labels {
		labels += fmt.Sprintf("\tLKey: %s\t LValue: %s\n", key, value)
	}
	return fmt.Sprintf("ID:%v\nImage:%v\nLabels:\n%v", c.ShortId, c.Image, labels)
}
