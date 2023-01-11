package traefik

import (
	"encoding/json"
	"go.uber.org/zap"
	"net/http"
)

type TraefikClient struct {
	http.Client
	Ip string
}

func NewTraefikClient(ip string) *TraefikClient {
	return &TraefikClient{Client: http.Client{}, Ip: ip}
}

func (c *TraefikClient) GetCountOfServices(log *zap.Logger) int {
	req, err := http.NewRequest("GET", "http://"+c.Ip+":8080/api/http/services", nil)
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
