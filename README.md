# Nocloud-operator
___
### Nocloud-operator - util for updating container and images

To get start you need __operator-config.yaml__ file

```yaml
duration: 30
composePrefix: "docker-operator_"
```
__Duration__ - the amount of time in __minutes__ after which the operator will start the update

__ComposePrefix__ - name of project where you start you containers 