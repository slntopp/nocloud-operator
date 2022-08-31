package main

import (
	"github.com/joho/godotenv"
	dockerOperator "github.com/slntopp/nocloud-operator/pkg/operator"
	"github.com/slntopp/nocloud/pkg/nocloud"
	"github.com/slntopp/nocloud/pkg/nocloud/auth"
	"github.com/slntopp/nocloud/pkg/nocloud/schema"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

var (
	log         *zap.Logger
	SIGNING_KEY []byte
)

func init() {
	log = nocloud.NewLogger()

	viper.SetDefault("SIGNING_KEY", "seeeecreet")
	SIGNING_KEY = []byte(viper.GetString("SIGNING_KEY"))
}

func main() {
	defer func() {
		_ = log.Sync()
	}()

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file", zap.Error(err))
	}

	auth.SetContext(log, SIGNING_KEY)
	token, err := auth.MakeToken(schema.ROOT_ACCOUNT_KEY)
	if err != nil {
		log.Fatal(err.Error())
	}

	operator := dockerOperator.NewOperator(log, token)
	operator.Wait()

	err = operator.ConfigureDns()
	if err != nil {
		log.Fatal("Error Configuring DNS", zap.Error(err))
	}

	containers := operator.Ps()
	for _, container := range containers {
		log.Info("Found Container", zap.String("name", container.Names[0]), zap.String("image", container.Image), zap.String("id", container.ShortId))
	}

	operator.ConnectToTraefik()

	operator.ObserveContainers()
}
