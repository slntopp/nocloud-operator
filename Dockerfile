ARG APP_PATH=/go/src/github.com/slntopp/nocloud-operator

FROM golang:1.18-alpine as builder
ARG APP_PATH

RUN apk add upx

WORKDIR ${APP_PATH}
COPY . ${APP_PATH}

RUN CGO_ENABLED=0 go build -o operator ./cmd/operator/main.go
RUN upx ./operator

FROM scratch
ARG APP_PATH

WORKDIR /
COPY --from=builder ${APP_PATH}/operator /operator

LABEL org.opencontainers.image.source https://github.com/slntopp/nocloud-operator

ENTRYPOINT ["/operator"]
