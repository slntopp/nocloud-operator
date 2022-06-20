FROM golang:1.18-alpine

ENV PROJECT_REPO=github.com/slntopp/nocloud-operator
ENV APP_PATH=/go/src/${PROJECT_REPO}

RUN apk add upx

WORKDIR ${APP_PATH}
COPY . ${APP_PATH}

RUN CGO_ENABLED=0 GOOS=linux go build -o operator ./cmd/operator/main.go
RUN upx ./operator

FROM scratch

ENV PROJECT_REPO=github.com/slntopp/nocloud-operator
ENV APP_PATH=/go/src/${PROJECT_REPO}

WORKDIR /
COPY --from=0 ${APP_PATH}/operator /operator
COPY --from=0 ${APP_PATH}/.env /.env

LABEL org.opencontainers.image.source https://github.com/slntopp/nocloud-operator

ENTRYPOINT ["/operator"]
