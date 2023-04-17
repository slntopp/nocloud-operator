package main

import (
	"github.com/joho/godotenv"
	dockerOperator "github.com/slntopp/nocloud-operator/pkg/operator"
	"github.com/slntopp/nocloud/pkg/nocloud"
	"github.com/slntopp/nocloud/pkg/nocloud/auth"
	"github.com/slntopp/nocloud/pkg/nocloud/schema"
	"go.uber.org/zap"
	"os"
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

	SIGNING_KEY := []byte(os.Getenv("SIGNING_KEY"))

	auth.SetContext(log, SIGNING_KEY)
	token, err := auth.MakeToken(schema.ROOT_ACCOUNT_KEY)
	if err != nil {
		log.Fatal(err.Error())
	}

	operator := dockerOperator.NewOperator(log, token)
	err = operator.ConfigureDns()
	if err != nil {
		log.Fatal("Error Configuring DNS", zap.Error(err))
	}

	err = operator.SetDnsIpToContainers()
	if err != nil {
		log.Fatal("Error Set Ip DNS", zap.Error(err))
	}

	containers := operator.Ps()
	for _, container := range containers {
		log.Info("Found Container", zap.String("name", container.Names[0]), zap.String("image", container.Image), zap.String("id", container.ShortId))
	}

	/*
		err = operator.ConnectToTraefik("proxy")
		if err != nil {
			log.Fatal(err.Error())
		}
	*/

	operator.ObserveContainers()
}
