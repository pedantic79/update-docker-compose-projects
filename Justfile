EXE:="update-docker-compose-projects"

build-all: #!/usr/bin/env bash
    for i in $(jq -r 'to_entries|.[].key' .mapping.json); do just build $i; done

copy hostname:
    scp {{EXE}} {{hostname}}:~/bin

build hostname:
    just build-with-arch "{{hostname}}" $(jq -r --arg name "{{hostname}}" '.[$name]' .mapping.json)

build-with-arch hostname arch: (_build-arch arch) (copy hostname) clean

_build-arch arch:
    CGO_ENABLED=0 GOOS=linux GOARCH={{arch}} go build -o {{EXE}} .

clean:
    rm -f {{EXE}}

fmt:
    find . \( -path ./.git -o -path ./vendor \) -prune -o -type f -name '*.go' -exec gofmt -w {} +

fmt-check:
    #!/usr/bin/env bash
    set -euo pipefail
    unformatted="$(find . \( -path ./.git -o -path ./vendor \) -prune -o -type f -name '*.go' -exec gofmt -l {} +)"
    if [[ -n "$unformatted" ]]; then
        echo "Go files need formatting:"
        echo "$unformatted"
        exit 1
    fi

lint:
    golangci-lint run ./...

test:
    go test -race ./...

coverage:
    go test -covermode=atomic -coverprofile=coverage.out ./...
    go tool cover -func=coverage.out

check: fmt-check lint test
