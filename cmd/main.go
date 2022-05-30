package main

import (
	dockerOperator "github.com/gorobot-nz/docker-operator/pkg/operator"

	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
)

func main() {

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	operator := dockerOperator.NewOperator()
	containers := operator.Ps()
	for _, container := range containers {
		log.Println(container)
	}

	operator.ObserveContainers()
}
