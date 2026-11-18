.PHONY: build
build: 
	go generate ./...
	sam build --parameter-overrides architecture=x86_64

.PHONY: local
local: build
	sam local start-api --docker-network icaa-shared --add-host host.docker.internal:host-gateway --parameter-overrides architecture=x86_64 --warm-containers EAGER --env-vars env.json -p 3006
