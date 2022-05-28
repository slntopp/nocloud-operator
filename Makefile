nginx:
	docker run --network=docker-operator_proxy -v proxy:/etc/nginx/templates --link health --link registry --link registry --link services-registry --link sp-registry --link settings --link dns-mgmt --link edge --link billing -p 8080:8080 --name=nginx nginx

health:
	docker run --name=health-service -e LOG_LEVEL=-1 -e SIGNING_KEY=qwe -e PROBABLES=registry:8080,billing:8080,services-registry:8080,sp-registry:8080,settings:8080,dns-mgmt:8080,edge:8080 ghcr.io/slntopp/nocloud/health:latest

health_rm:
	docker stop health-service && docker rm health-service

health_inspect_base:
	docker inspect health-service > reports/base_health.json

health_inspect_another:
	docker inspect health-service > reports/another_health.json

health_connect:
	docker network connect nocloud_n_ione_grpc-internal health-service && docker network connect nocloud_n_ione_proxy health-service