.PHONY: build test vet fmt lint tidy vue-install vue-build verify clean

BINARY := build/vuepher

build:
	go build -o $(BINARY) ./cmd/vuepher

install:
	go install ./cmd/vuepher

test:
	go test ./... -count=1

vet:
	go vet ./...

fmt:
	gofmt -s -w .

tidy:
	go mod tidy

vue-install:
	cd packages/vue3 && npm install

vue-build:
	cd packages/vue3 && npm run build

vue-typecheck:
	cd packages/vue3 && npm run typecheck

# One command to check the whole repo before pushing.
verify: fmt vet test vue-typecheck

clean:
	rm -rf build packages/vue3/dist
