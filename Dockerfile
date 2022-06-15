FROM golang:latest
ENV PROJECT_REPO=github.com/slntopp/nocloud-operator
ENV APP_PATH=/go/src/${PROJECT_REPO}
WORKDIR ${APP_PATH}
COPY . ${APP_PATH}
RUN CGO_ENABLED=0 GOOS=linux go build -o operator ./cmd/operator/main.go


FROM scratch
ENV PROJECT_REPO=github.com/slntopp/nocloud-operator
ENV APP_PATH=/go/src/${PROJECT_REPO}
WORKDIR ${APP_PATH}
COPY --from=0 ${APP_PATH}/operator ${APP_PATH}/operator
COPY --from=0 ${APP_PATH}/operator-config.yaml ${APP_PATH}/operator-config.yaml
COPY --from=0 ${APP_PATH}/.env ${APP_PATH}/.env

ENTRYPOINT ["./operator"]
