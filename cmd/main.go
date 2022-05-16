package main

import (
	dockerOperator "github.com/gorobot-nz/docker-operator/pkg/operator"
	log "github.com/sirupsen/logrus"
)

func main() {
	operator := dockerOperator.NewOperator()

	operator.ReadConfig("docker-compose.yml")

	containers := operator.Ps()
	for _, container := range containers {
		log.Println(container)
	}

	operator.ObserveContainers()
}
