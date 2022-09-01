# NoCloud Operator

NoCloud platform maintenance helper.

## Setup

To get start you need __operator-config.yaml__ file

```yaml
duration: 10
composePrefix: "nocloud-operator_"

registries:
#   username: "username"
#   password: "pass"
#   serverAddress: "ghcr.io"

dns:
  -8.8.8.8
  -8.8.4.4
```

__Duration__ - the amount of time in __seconds__ after which the operator will start the update

__ComposePrefix__ - name of project where you start you containers

__Username__, __Password__, __ServerAddress__ - credentials for docker

__DNS__ - array of default dns ips

### Example of docker-compose file for operator

```yaml
version: "3.8"
services:
  operator:
    env_file:
      - .env
    container_name: operator
    image: ghcr.io/slntopp/nocloud/operator:latest
    restart: always
    volumes:
      - ./operator-config.yml:/operator-config.yml
      - ./docker-compose.yml:/docker-compose.yml
      - /var/run/docker.sock:/var/run/docker.sock
```

## Configure details

See [Labels reference](LABELS.md) to learn how to configure operators behaviour.
