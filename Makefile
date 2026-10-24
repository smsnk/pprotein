.PHONY: noop
noop:

.PHONY: run
run:
	go run ./cli/pprotein

.PHONY: run-agent
run-agent:
	go run ./cli/pprotein-agent

.PHONY: build
build: pprotein pprotein-agent pprotein-cli

pprotein: view/dist
	go build -trimpath -ldflags="-w -s" ./cli/pprotein

pprotein-agent:
	go build -trimpath -ldflags="-w -s" ./cli/pprotein-agent

pprotein-cli:
	go build -trimpath -ldflags="-w -s" ./cli/pprotein-cli

view/dist:
	npm --prefix view ci
	npm --prefix view run build

# docs/cli/ の pprotein-cli のリファレンスをコマンドの定義から作り直す
.PHONY: docs
docs:
	go test ./cli/pprotein-cli -run TestReference -update

.PHONY: clean
clean:
	rm -rf pprotein pprotein-agent pprotein-cli view/dist
