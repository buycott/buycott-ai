.PHONY: all build vet test clean \
        docker-build \
        compose-setup compose-up compose-down compose-restart compose-logs compose-ps \
        compose-status compose-conversation \
        k8s-configure k8s-apply k8s-delete k8s-status \
        k8s-logs-pipeline k8s-logs-dashboard \
        run status logs conversation help

# ── Variables ─────────────────────────────────────────────────────────────────
IMAGE_NAME ?= buycott
IMAGE_TAG  ?= latest
IMAGE      := $(IMAGE_NAME):$(IMAGE_TAG)
CONFIG     ?= config.yaml
DIRECTION  ?= Build a sample web application

_NS = $(shell cat k8s/manifests/.namespace 2>/dev/null || echo buycott)

# ── Go ────────────────────────────────────────────────────────────────────────
all: build

build:
	@mkdir -p bin
	GOTOOLCHAIN=auto go build -o bin/buycott ./

vet:
	GOTOOLCHAIN=auto go vet ./...

test:
	GOTOOLCHAIN=auto go test ./...

clean:
	rm -rf bin/

# ── Docker ────────────────────────────────────────────────────────────────────
docker-build:
	docker build -t $(IMAGE) .

# ── Docker Compose ────────────────────────────────────────────────────────────
# Copies config.example.yaml → config.yaml and creates ./artifacts if missing.
compose-setup:
	@if [ ! -f config.yaml ]; then \
	    echo "Copying config.example.yaml → config.yaml"; \
	    cp config.example.yaml config.yaml; \
	fi
	@mkdir -p artifacts
	@echo "Ready. Edit config.yaml, then: make compose-up DIRECTION='your direction'"

compose-up: compose-setup
	BUYCOTT_DIRECTION="$(DIRECTION)" BUYCOTT_IMAGE="$(IMAGE)" docker compose up -d --build
	@echo ""
	@echo "  Pipeline gRPC: localhost:$${BUYCOTT_API_PORT:-8080}"
	@echo "  Dashboard:     http://localhost:$${BUYCOTT_DASHBOARD_PORT:-8000}"

compose-down:
	docker compose down

compose-restart:
	docker compose restart

compose-logs:
	docker compose logs -f

compose-ps:
	docker compose ps

compose-status:
	docker compose exec pipeline buycott status --config /etc/buycott/config.yaml

compose-conversation:
	docker compose exec pipeline buycott conversation --config /etc/buycott/config.yaml --limit 10

# ── Kubernetes ────────────────────────────────────────────────────────────────
k8s-configure:
	@bash scripts/configure-k8s.sh

k8s-apply:
	kubectl apply -f k8s/manifests/

k8s-delete:
	kubectl delete -f k8s/manifests/ --ignore-not-found
	kubectl delete namespace $(_NS) --ignore-not-found

k8s-status:
	kubectl get all,pvc,configmap,secret -n $(_NS)

k8s-logs-pipeline:
	kubectl logs -n $(_NS) -l app=buycott-pipeline -f --tail=100

k8s-logs-dashboard:
	kubectl logs -n $(_NS) -l app=buycott-dashboard -f --tail=100

# ── Local development ─────────────────────────────────────────────────────────
run: build
	./bin/buycott start --config $(CONFIG) "$(DIRECTION)"

status: build
	./bin/buycott status --config $(CONFIG)

logs: build
	./bin/buycott logs --config $(CONFIG)

conversation: build
	./bin/buycott conversation --config $(CONFIG)

# ── Help ──────────────────────────────────────────────────────────────────────
help:
	@echo ""
	@echo "Buycott — Multi-model Task Pipeline"
	@echo "===================================="
	@echo ""
	@echo "Go:"
	@echo "  make build              Build binary → bin/buycott"
	@echo "  make vet                go vet ./..."
	@echo "  make test               go test ./..."
	@echo "  make clean              Remove bin/"
	@echo ""
	@echo "Docker:"
	@echo "  make docker-build       Build image  (IMAGE=$(IMAGE))"
	@echo ""
	@echo "Docker Compose:"
	@echo "  make compose-setup      Init config.yaml + artifacts/"
	@echo "  make compose-up         Start all services  DIRECTION='...'"
	@echo "  make compose-down       Stop all services"
	@echo "  make compose-restart    Restart services"
	@echo "  make compose-logs       Stream all logs"
	@echo "  make compose-ps         Show service status"
	@echo "  make compose-status     Show pipeline status via buycott status"
	@echo "  make compose-conversation  Recent conversation logs"
	@echo ""
	@echo "Kubernetes:"
	@echo "  make k8s-configure      Interactive manifest generator"
	@echo "  make k8s-apply          kubectl apply -f k8s/manifests/"
	@echo "  make k8s-delete         Remove all Buycott resources + namespace"
	@echo "  make k8s-status         kubectl get all in namespace"
	@echo "  make k8s-logs-pipeline  Stream pipeline pod logs"
	@echo "  make k8s-logs-dashboard Stream dashboard pod logs"
	@echo ""
	@echo "Local dev (requires $(CONFIG)):"
	@echo "  make run                Build + start pipeline  DIRECTION='...'"
	@echo "  make status             Show status"
	@echo "  make logs               Show event log"
	@echo "  make conversation       Show conversation log"
	@echo ""
	@echo "Variables:"
	@echo "  IMAGE_NAME=$(IMAGE_NAME)  IMAGE_TAG=$(IMAGE_TAG)  IMAGE=$(IMAGE)"
	@echo "  CONFIG=$(CONFIG)  DIRECTION=$(DIRECTION)"
	@echo ""
