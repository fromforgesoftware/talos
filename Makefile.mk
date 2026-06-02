# Hallmark service targets. Self-contained; can be run directly
# (`make -f services/hallmark/Makefile.mk hallmark-build`) or included by the
# root Makefile.
HALLMARK_DIR ?= services/hallmark

.PHONY: hallmark-proto hallmark-build hallmark-test hallmark-vet hallmark-lint hallmark-migrate hallmark-run

hallmark-proto:        ## Regenerate Go code from the Hallmark protos
	cd $(HALLMARK_DIR) && buf generate

hallmark-lint:         ## Lint the Hallmark protos
	cd $(HALLMARK_DIR) && buf lint

hallmark-build:        ## Build all Hallmark packages
	cd $(HALLMARK_DIR) && go build ./...

hallmark-vet:          ## Vet the Hallmark module
	cd $(HALLMARK_DIR) && go vet ./...

hallmark-test:         ## Run Hallmark unit tests
	cd $(HALLMARK_DIR) && go test ./...

HALLMARK_LOCAL_ENV := SVC_NAME=hallmark REST_ADDRESS=:8080 HTTP_ADDRESS=:8080 GRPC_ADDRESS=:9090

hallmark-migrate:      ## Apply Hallmark migrations (reads DB_* env)
	cd $(HALLMARK_DIR) && go run ./cmd/migrator

hallmark-run:          ## Run the Hallmark server locally
	cd $(HALLMARK_DIR) && $(HALLMARK_LOCAL_ENV) go run ./cmd/server
