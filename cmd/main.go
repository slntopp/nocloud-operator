package main

import (
	dockerOperator "github.com/gorobot-nz/docker-operator/pkg/operator"
)

func main() {
	operator := dockerOperator.NewOperator()

	operator.Ps()
	/*for _, container := range containers {
		log.Println(container)
	}*/

	operator.ObserveContainers()
}
