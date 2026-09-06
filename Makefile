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

# Fixed pre-draw profiles for controlled experiments. Replace completed test
# entries on Arena to avoid accumulating bots; defaults keep the original policy.
ONYX_OPEN ?= baseline
ONYX_DEFENSE ?= original
ONYX_LDFLAGS = -X github.com/nuttakit/2-7-bot/internal/onyx.predrawProfile=$(ONYX_OPEN) \
	-X github.com/nuttakit/2-7-bot/internal/onyx.predrawDefense=$(ONYX_DEFENSE)

.PHONY: help arena bot bot-release bot-model-release bot-model test fmt vet docs-check engine spar

help:
	@echo 'arena       build the harness CLI into bin/arena'
	@echo 'bot         build the bot for this host, into bin/bot'
	@echo 'bot-release build the static linux artifact to upload'
	@echo 'bot-model-release build modeled betting with local model and blueprint assets'
	@echo 'test        go test ./...'
	@echo 'fmt vet     go fmt / go vet'
	@echo 'docs-check  verify vendored protocol docs match upstream $(ENGINE_SHA)'
	@echo 'engine      clone + build the upstream poker-arena CLI'
	@echo 'spar        run BOT against builtin:random locally'

arena:
	go build -o bin/arena ./cmd/arena

# For sparring: a host-native build, so the local engine can spawn it.
bot:
	go build -ldflags='$(ONYX_LDFLAGS)' -o bin/bot ./cmd/bot

# The upload artifact. One static Linux x86-64 ELF, named for the bot
# (docs/naming.md) because `arena upload --name` defaults to the filename.
BOT_NAME ?= 2-7-garnet-1

bot-release:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	  go build -trimpath -ldflags='-s -w $(ONYX_LDFLAGS)' -o bin/$(BOT_NAME) ./cmd/bot

# Generated assets stay in the ignored bin directory. The overlay substitutes
# them only for this build and leaves the baseline embedded assets untouched.
MODEL_POLICY ?= bin/swit-all-policy.json.gz
MODEL_BLUEPRINT ?= bin/garnet.bin.gz
MODEL_BOT_NAME ?= 2-7-garnet-1
MODEL_SELECTION ?= mode
MODEL_PROFILE ?= learned
MODEL_ALPHA ?= 1
MODEL_PARTICLES ?= 512
MODEL_FIXED ?= button
MODEL_LDFLAGS = $(ONYX_LDFLAGS) -X main.playerProfile=$(MODEL_PROFILE) \
	-X github.com/nuttakit/2-7-bot/internal/onyx.modelSelection=$(MODEL_SELECTION) \
	-X github.com/nuttakit/2-7-bot/internal/onyx.riverResponseAlpha=$(MODEL_ALPHA) \
	-X github.com/nuttakit/2-7-bot/internal/onyx.beliefParticleCount=$(MODEL_PARTICLES) \
	-X github.com/nuttakit/2-7-bot/internal/cfr.fixedProfile=$(MODEL_FIXED)
MODEL_OVERLAY = python3 -c 'import json,pathlib,sys; r=pathlib.Path.cwd(); paths=["internal/onyx/opponent_policy.json","internal/lapis/blueprint.bin.gz"]; pathlib.Path(sys.argv[1]).write_text(json.dumps({"Replace":{str(r/p):str(pathlib.Path(v).resolve()) for p,v in zip(paths,sys.argv[2:])}}))'

bot-model-release:
	@test -f "$(MODEL_POLICY)" && test -f "$(MODEL_BLUEPRINT)"
	@mkdir -p bin
	@model_overlay=$$(mktemp "$$(pwd)/bin/model-overlay.XXXXXX"); \
	  trap 'rm -f "$$model_overlay"' EXIT; \
	  $(MODEL_OVERLAY) "$$model_overlay" "$(MODEL_POLICY)" "$(MODEL_BLUEPRINT)" && \
	  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -overlay "$$model_overlay" \
	    -ldflags='-s -w $(MODEL_LDFLAGS)' -o bin/$(MODEL_BOT_NAME) ./cmd/bot

# The same build for this host, into bin/bot-model, so the local engine can
# spar it (bin/diag/spar.sh) before anything is uploaded.
bot-model:
	@test -f "$(MODEL_POLICY)" && test -f "$(MODEL_BLUEPRINT)"
	@mkdir -p bin
	@model_overlay=$$(mktemp "$$(pwd)/bin/model-overlay.XXXXXX"); \
	  trap 'rm -f "$$model_overlay"' EXIT; \
	  $(MODEL_OVERLAY) "$$model_overlay" "$(MODEL_POLICY)" "$(MODEL_BLUEPRINT)" && \
	  go build -overlay "$$model_overlay" -ldflags='$(MODEL_LDFLAGS)' -o bin/bot-model ./cmd/bot

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

# Reproduce the targeted blueprint; generated strategy data stays local.
.PHONY: blueprint
blueprint:
	go run ./cmd/cfrgen train -model cobalt -weight 1 -iters 100000 \
	  -workers 1 -seed 1 -minvisits 20 -out internal/lapis/blueprint.bin.gz
