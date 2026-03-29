.PHONY: build run stop clean

CONTROLLER_PORT := 8180
AGENT_PORTS := 8181 8182 8183
LP_PORTS := 9222 9223 9224
LP_BIN := $(shell which lightpanda)

build:
	@mkdir -p bin
	go build -o bin/controller ./cmd/controller
	go build -o bin/agent ./cmd/agent
	@echo "built bin/controller + bin/agent"

run: build stop
	@echo "launching shoal cluster..."
	@bin/controller -addr :$(CONTROLLER_PORT) > /dev/null 2>&1 & echo "  controller on :$(CONTROLLER_PORT) (pid $$!)"
	@sleep 0.3
	@for i in 0 1 2; do \
		agent_port=$$(echo $(AGENT_PORTS) | cut -d' ' -f$$((i+1))); \
		lp_port=$$(echo $(LP_PORTS) | cut -d' ' -f$$((i+1))); \
		bin/agent -addr :$$agent_port \
			-controller http://localhost:$(CONTROLLER_PORT) \
			-backend lightpanda \
			-lightpanda-bin $(LP_BIN) \
			-lightpanda-port $$lp_port \
			> /dev/null 2>&1 & \
		echo "  agent on :$$agent_port -> lightpanda :$$lp_port (pid $$!)"; \
	done
	@sleep 2
	@echo ""
	@echo "pool status:"
	@curl -s localhost:$(CONTROLLER_PORT)/pool/status | python3 -m json.tool
	@echo ""
	@echo "shoal is running. make stop to tear down."

stop:
	@fuser -k 8180/tcp 8181/tcp 8182/tcp 8183/tcp 9222/tcp 9223/tcp 9224/tcp 2>/dev/null || true
	@sleep 0.3
	@echo "stopped"

clean: stop
	rm -rf bin/
