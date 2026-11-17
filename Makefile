.PHONY: up demo-gateway apply-cilium apply-tetragon events reset build-image load-image deploy

KIND_CLUSTER ?= mcp-demo
NAMESPACE ?= mcp-demo
GATEWAY_URL ?= http://localhost:30090/mcp

up: build-image ## Create local Kind cluster and deploy demo workloads
	@command -v kind >/dev/null 2>&1 || { echo "kind is required: https://kind.sigs.k8s.io/"; exit 1; }
	@command -v kubectl >/dev/null 2>&1 || { echo "kubectl is required"; exit 1; }
	@kind get clusters | grep -qx $(KIND_CLUSTER) || kind create cluster --name $(KIND_CLUSTER)
	@$(MAKE) load-image
	@$(MAKE) deploy
	@echo "Cluster ready. Gateway expected at $(GATEWAY_URL) once port-mapping/NodePort is reachable."

build-image:
	docker build -t mcp-gateway:local ./assets/gateway

load-image: build-image
	kind load docker-image mcp-gateway:local --name $(KIND_CLUSTER)

deploy:
	kubectl apply -f assets/gateway/k8s.yaml
	kubectl -n $(NAMESPACE) rollout status deploy/approved-backend --timeout=180s
	kubectl -n $(NAMESPACE) rollout status deploy/mcp-gateway --timeout=180s
	kubectl -n $(NAMESPACE) rollout status deploy/mcp-tool-runner --timeout=180s

demo-gateway: ## Run the gateway approval demo script
	./assets/scripts/trigger-high-risk.sh

apply-cilium: ## Apply CiliumNetworkPolicy for tool-runner egress restriction
	kubectl apply -f assets/policies/cnp-mcp-egress.yaml
	./assets/scripts/test-exfiltration.sh

apply-tetragon: ## Apply Tetragon runtime policy to block shells in tool-runner
	kubectl apply -f assets/policies/tp-block-shell.yaml
	./assets/scripts/test-shell-escape.sh

events: ## Show recent Tetragon events for mcp-demo namespace (requires tetra in tetragon pod)
	kubectl -n kube-system exec ds/tetragon -c tetragon -- tetra getevents -o compact --namespace $(NAMESPACE) --since 10m || true

reset: ## Remove demo resources and policies
	-kubectl delete -f assets/policies/tp-block-shell.yaml --ignore-not-found
	-kubectl delete -f assets/policies/cnp-mcp-egress.yaml --ignore-not-found
	-kubectl delete -f assets/gateway/k8s.yaml --ignore-not-found
	@echo "Reset complete. To delete the Kind cluster: kind delete cluster --name $(KIND_CLUSTER)"
