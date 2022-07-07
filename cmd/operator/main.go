package main

import (
	"github.com/joho/godotenv"
	dockerOperator "github.com/slntopp/nocloud-operator/pkg/operator"
	"github.com/slntopp/nocloud/pkg/nocloud"
	"go.uber.org/zap"
)

var (
	log *zap.Logger
)

func init() {
	log = nocloud.NewLogger()
}

func main() {
	defer func() {
		_ = log.Sync()
	}()

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file", zap.Error(err))
	}

	operator := dockerOperator.NewOperator(log)
	err = operator.ConfigureDns()
	if err != nil {
		log.Fatal("Error Configuring DNS", zap.Error(err))
	}

	containers := operator.Ps()
	for _, container := range containers {
		log.Info("Found Container", zap.String("container", container.String()))
	}

	operator.ObserveContainers()
}
