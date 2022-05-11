package main

import (
	dockerOperator "github.com/gorobot-nz/docker-operator/pkg/operator"
	log "github.com/sirupsen/logrus"
)

func main() {
	log.SetLevel(log.DebugLevel)
	operator := dockerOperator.NewOperator()

	containers := operator.Ps()

	for _, container := range containers {
		log.Println(container)
	}

	operator.ObserveContainers()
}
