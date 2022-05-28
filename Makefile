run:
	docker run --network=docker-operator_proxy -v proxy:/etc/nginx/templates --link health --link registry --link registry --link services-registry --link sp-registry --link settings --link dns-mgmt --link edge --link billing -p 8080:8080 --name=nginx nginx
