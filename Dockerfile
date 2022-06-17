FROM golang:latest
ENV PROJECT_REPO=github.com/slntopp/nocloud-operator
ENV APP_PATH=/go/src/${PROJECT_REPO}
WORKDIR ${APP_PATH}
COPY . ${APP_PATH}
RUN CGO_ENABLED=0 GOOS=linux go build -o operator ./cmd/operator/main.go


FROM scratch
ENV PROJECT_REPO=github.com/slntopp/nocloud-operator
ENV APP_PATH=/go/src/${PROJECT_REPO}
WORKDIR .
COPY --from=0 ${APP_PATH}/operator ./operator
COPY --from=0 ${APP_PATH}/.env ./.env

ENTRYPOINT ["./operator"]
