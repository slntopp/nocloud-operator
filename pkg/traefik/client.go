package traefik

import (
	"encoding/json"
	"fmt"
	"go.uber.org/zap"
	"net/http"
)

type TraefikClient struct {
	http.Client
	Host string
}

func NewTraefikClient(host string) *TraefikClient {
	return &TraefikClient{Client: http.Client{}, Host: host}
}

func (c *TraefikClient) Ping() error {
	req, err := http.NewRequest("GET", fmt.Sprintf("https://%s:8080/api/version", c.Host), nil)
	if err != nil {
		return err
	}
	_, err = c.Do(req)
	if err != nil {
		return err
	}

	return nil
}

func (c *TraefikClient) GetCountOfServices(log *zap.Logger) int {
	req, err := http.NewRequest("GET", fmt.Sprintf("https://%s:8080/api/http/services", c.Host), nil)
	if err != nil {
		return 0
	}
	resp, err := c.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	body := make([]byte, 4096)
	n, _ := resp.Body.Read(body)

	var services []TraefikService

	json.Unmarshal(body[:n], &services)

	var counter = 0

	for _, item := range services {
		if item.Provider == "docker" {
			log.Info("container", zap.String("name", item.Name))
			counter += 1
		}
	}
	return counter
}
