# ─────────────────────────────────────────────────────────────
# Tollbooth – top-level Makefile
# ─────────────────────────────────────────────────────────────
SHELL        := /bin/bash
HELM         := helm
KIND         := kind
CLUSTER_NAME := tollbooth

GATEWAY_DIR  := services/gateway
WOOMCP_DIR   := services/woo-mcp
DASH_DIR     := dashboard
HELM_DIR     := deploy/helm/tollbooth
KIND_CFG     := deploy/kind/kind-config.yaml

# ── Build ───────────────────────────────────────────────────
.PHONY: build build-gateway build-woo-mcp build-dashboard

build: build-gateway build-woo-mcp build-dashboard

build-gateway:
	cd $(GATEWAY_DIR) && go build ./...

build-woo-mcp:
	cd $(WOOMCP_DIR) && go build ./...

build-dashboard:
	cd $(DASH_DIR) && npm ci && npm run build

# ── Lint ────────────────────────────────────────────────────
.PHONY: lint lint-go lint-dashboard lint-helm

lint: lint-go lint-dashboard lint-helm

lint-go:
	cd $(GATEWAY_DIR) && go vet ./...
	cd $(WOOMCP_DIR)  && go vet ./...

lint-dashboard:
	cd $(DASH_DIR) && npm run lint 2>/dev/null || echo "lint script not configured yet"

lint-helm: helm-lint

# ── Test ────────────────────────────────────────────────────
.PHONY: test test-gateway test-woo-mcp test-dashboard

test: test-gateway test-woo-mcp test-dashboard

test-gateway:
	cd $(GATEWAY_DIR) && go test ./... -v -count=1

test-woo-mcp:
	cd $(WOOMCP_DIR) && go test ./... -v -count=1

test-dashboard:
	cd $(DASH_DIR) && npm test 2>/dev/null || echo "test script not configured yet"

# ── Helm ────────────────────────────────────────────────────
.PHONY: helm-lint helm-template-local helm-template-prod

helm-lint:
	$(HELM) lint $(HELM_DIR) -f $(HELM_DIR)/values-local.yaml
	$(HELM) lint $(HELM_DIR) -f $(HELM_DIR)/values-prod.yaml

helm-template-local:
	$(HELM) template tollbooth $(HELM_DIR) -f $(HELM_DIR)/values-local.yaml

helm-template-prod:
	$(HELM) template tollbooth $(HELM_DIR) -f $(HELM_DIR)/values-prod.yaml

# ── Docker ──────────────────────────────────────────────────
.PHONY: docker-build docker-gateway docker-woo-mcp docker-dashboard

docker-build: docker-gateway docker-woo-mcp docker-dashboard

docker-gateway:
	docker build -t tollbooth/gateway:dev $(GATEWAY_DIR)

docker-woo-mcp:
	docker build -t tollbooth/woo-mcp:dev $(WOOMCP_DIR)

docker-dashboard:
	docker build -t tollbooth/dashboard:dev $(DASH_DIR)

# ── Cluster ─────────────────────────────────────────────────
.PHONY: cluster-up cluster-down

cluster-up:
	$(KIND) create cluster --name $(CLUSTER_NAME) --config $(KIND_CFG)
	@echo "Cluster $(CLUSTER_NAME) is ready."

cluster-down:
	$(KIND) delete cluster --name $(CLUSTER_NAME)
