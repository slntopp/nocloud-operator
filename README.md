# Nocloud-operator

___

## Nocloud-operator - util for updating container and images

To get start you need __operator-config.yaml__ file

```yaml
duration: 30
composePrefix: "docker-operator_"
```
__Duration__ - the amount of time in __seconds__ after which the operator will start the update

__ComposePrefix__ - name of project where you start you containers 

### Example of docker-compose file for operator

```yaml
version: "3.8"
services:
  operator:
    environment:
      - all env variables used in your project
    container_name: operator
    image: operator:latest
    restart: always
    volumes:
      - ./operator-config.yml:/go/src/github.com/slntopp/nocloud-operator/operator-config.yml
      - ./docker-compose.yml:/go/src/github.com/slntopp/nocloud-operator/docker-compose.yml
      - /var/run/docker.sock:/var/run/docker.sock
```