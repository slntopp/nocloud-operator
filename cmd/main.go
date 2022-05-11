package main

import (
	"fmt"
	dockerOperator "github.com/gorobot-nz/docker-operator/pkg/operator"
)

func main() {
	operator := dockerOperator.NewOperator()

	containers := operator.PsContainers()

	for _, container := range containers {
		fmt.Println(container.ID)
	}
}
