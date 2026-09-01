# MixedSolver Arena harness.
#
# The platform contract lives in docs/ — see docs/SOURCES.md for provenance.

ENGINE_REPO := https://github.com/mixedsolver/poker-arena
ENGINE_SHA  := 80c7eeb758b05fd957063330747c4f234f77a0f8
ENGINE_DIR  := third_party/poker-arena
ENGINE_BIN  := $(ENGINE_DIR)/target/release/poker-arena

GAME  ?= 27td-fl
HANDS ?= 100
BOT   ?= ./bin/bot

.PHONY: help arena bot bot-release test fmt vet docs-check engine spar

help:
	@echo 'arena       build the harness CLI into bin/arena'
	@echo 'bot         build the bot for this host, into bin/bot'
	@echo 'bot-release build the static linux artifact to upload'
	@echo 'test        go test ./...'
	@echo 'fmt vet     go fmt / go vet'
	@echo 'docs-check  verify vendored protocol docs match upstream $(ENGINE_SHA)'
	@echo 'engine      clone + build the upstream poker-arena CLI'
	@echo 'spar        run BOT against builtin:random locally'

arena:
	go build -o bin/arena ./cmd/arena

# For sparring: a host-native build, so the local engine can spawn it.
bot:
	go build -o bin/bot ./cmd/bot

# The upload artifact. One static Linux x86-64 ELF, named for the bot
# (docs/naming.md) because `arena upload --name` defaults to the filename.
BOT_NAME ?= nutt-27td-fl-hu-h3

bot-release:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	  go build -trimpath -ldflags='-s -w' -o bin/$(BOT_NAME) ./cmd/bot

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

docs-check:
	./scripts/sync-docs.sh --check

engine:
	./scripts/build-engine.sh

spar: engine bot
	$(ENGINE_BIN) run \
	  --game $(GAME) \
	  --hands $(HANDS) \
	  --bot 'candidate@cmd:$(BOT)' \
	  --bot 'baseline@builtin:random' \
	  --timeout-ms 1000 \
	  --output json
