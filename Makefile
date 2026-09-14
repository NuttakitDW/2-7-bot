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
.PHONY: spar-spinel-6max upload-spinel-6max-dry-run

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
	@echo 'spar-spinel-6max run Spinel at every seat over 6 x HANDS hands'
	@echo 'upload-spinel-6max-dry-run validate the six-max upload offline'

arena:
	go build -o bin/arena ./cmd/arena

# For sparring: a host-native build, so the local engine can spawn it.
bot:
	go build -ldflags='$(ONYX_LDFLAGS)' -o bin/bot ./cmd/bot

# The upload artifact. One static Linux x86-64 ELF, named for the bot
# (docs/naming.md) because `arena upload --name` defaults to the filename.
BOT_NAME ?= 27-onyx-10

bot-release:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	  go build -trimpath -ldflags='-s -w $(ONYX_LDFLAGS)' -o bin/$(BOT_NAME) ./cmd/bot

# Generated assets stay in the ignored bin directory. The overlay substitutes
# them only for this build and leaves the baseline embedded assets untouched.
MODEL_POLICY ?= bin/onyx-swit-policy-history-large.json.gz
MODEL_BLUEPRINT ?= bin/onyx-78-blueprint.bin.gz
MODEL_BOT_NAME ?= 27-onyx-158
MODEL_SELECTION ?= mode
MODEL_PROFILE ?= model-bets
MODEL_ALPHA ?= 1
MODEL_PARTICLES ?= 512
MODEL_FIXED ?= none
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

# Azurite: the equity-bucketed self-play blueprint, selected by local
# exploitability (cfrgen exploit) rather than hosted matches. The blueprint
# is too large to commit; it stays in bin/ and is overlaid at build time,
# so the build flags here must match the ones it was trained with.
AZURITE_BUCKETS ?= 160,160,160
AZURITE_FIXED ?= none
AZURITE_BLUEPRINT ?= bin/azurite.bin.gz
AZURITE_BOT_NAME ?= 27-azurite-1
AZURITE_NATIVE_PATH ?= bin/bot-azurite
AZURITE_CFR_LDFLAGS = -X github.com/nuttakit/2-7-bot/internal/cfr.handProfile=equity \
	-X github.com/nuttakit/2-7-bot/internal/cfr.equityProfile=$(AZURITE_BUCKETS) \
	-X github.com/nuttakit/2-7-bot/internal/cfr.fixedProfile=$(AZURITE_FIXED)
AZURITE_PURIFY ?= 0.05
AZURITE_GREEDY ?= false
AZURITE_LDFLAGS = $(ONYX_LDFLAGS) -X main.playerProfile=azurite $(AZURITE_CFR_LDFLAGS) \
	-X github.com/nuttakit/2-7-bot/internal/lapis.Purify=$(AZURITE_PURIFY) \
	-X github.com/nuttakit/2-7-bot/internal/lapis.Greedy=$(AZURITE_GREEDY)
AZURITE_OVERLAY = python3 -c 'import json,pathlib,sys; r=pathlib.Path.cwd(); pathlib.Path(sys.argv[1]).write_text(json.dumps({"Replace":{str(r/"internal/lapis/blueprint.bin.gz"):str(pathlib.Path(sys.argv[2]).resolve())}}))'

.PHONY: cfrgen-azurite bot-azurite bot-azurite-release exploit-azurite

# The trainer and evaluator for the azurite abstraction, into bin/cfrgen-azurite.
cfrgen-azurite:
	go build -ldflags='$(AZURITE_CFR_LDFLAGS)' -o bin/cfrgen-azurite ./cmd/cfrgen

# Exploitability of the azurite blueprint over the abstract game.
exploit-azurite: cfrgen-azurite
	./bin/cfrgen-azurite exploit -bp $(AZURITE_BLUEPRINT) -purify 0.05 -fallback h3

bot-azurite:
	@test -f "$(AZURITE_BLUEPRINT)"
	@mkdir -p bin
	@overlay=$$(mktemp "$$(pwd)/bin/azurite-overlay.XXXXXX"); \
	  trap 'rm -f "$$overlay"' EXIT; \
	  $(AZURITE_OVERLAY) "$$overlay" "$(AZURITE_BLUEPRINT)" && \
	  go build -overlay "$$overlay" -ldflags='$(AZURITE_LDFLAGS)' -o $(AZURITE_NATIVE_PATH) ./cmd/bot

bot-azurite-release:
	@test -f "$(AZURITE_BLUEPRINT)"
	@mkdir -p bin
	@overlay=$$(mktemp "$$(pwd)/bin/azurite-overlay.XXXXXX"); \
	  trap 'rm -f "$$overlay"' EXIT; \
	  $(AZURITE_OVERLAY) "$$overlay" "$(AZURITE_BLUEPRINT)" && \
	  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -overlay "$$overlay" \
	    -ldflags='-s -w $(AZURITE_LDFLAGS)' -o bin/$(AZURITE_BOT_NAME) ./cmd/bot

# Reproduce the targeted blueprint; generated strategy data stays local.
.PHONY: blueprint
blueprint:
	go run ./cmd/cfrgen train -model cobalt -weight 1 -iters 100000 \
	  -workers 1 -seed 1 -minvisits 20 -out internal/lapis/blueprint.bin.gz

# Obsidian stores its per-street selection in the blueprint itself.
# Create it with cfrgen-azurite select; do not purify it a second time.
OBSIDIAN_BLUEPRINT ?= bin/obsidian/selected.bin.gz
OBSIDIAN_BOT_NAME ?= 27-obsidian-1
OBSIDIAN_BUCKETS ?= 160,160,160
OBSIDIAN_FIXED ?= none

.PHONY: bot-obsidian bot-obsidian-release
bot-obsidian:
	$(MAKE) bot-azurite AZURITE_BLUEPRINT=$(OBSIDIAN_BLUEPRINT) \
	  AZURITE_BUCKETS=$(OBSIDIAN_BUCKETS) AZURITE_FIXED=$(OBSIDIAN_FIXED) \
	  AZURITE_PURIFY=0 AZURITE_GREEDY=false AZURITE_NATIVE_PATH=bin/bot-obsidian

bot-obsidian-release:
	$(MAKE) bot-azurite-release AZURITE_BLUEPRINT=$(OBSIDIAN_BLUEPRINT) \
	  AZURITE_BUCKETS=$(OBSIDIAN_BUCKETS) AZURITE_FIXED=$(OBSIDIAN_FIXED) \
	  AZURITE_PURIFY=0 AZURITE_GREEDY=false AZURITE_BOT_NAME=$(OBSIDIAN_BOT_NAME)

# Tourmaline retains the fitted base policy and learns a sparse river response.
# The self-contained response JSON is generated by cfrgen river-train.
TOURMALINE_POLICY ?= bin/tourmaline/selected.json
TOURMALINE_BOT_NAME ?= 27-tourmaline-1
TOURMALINE_NATIVE_PATH ?= bin/bot-tourmaline
TOURMALINE_GREEDY ?= true
TOURMALINE_MIN_VISITS ?= 50
TOURMALINE_LDFLAGS = -X main.playerProfile=river-response \
	-X github.com/nuttakit/2-7-bot/internal/lapis.Greedy=$(TOURMALINE_GREEDY) \
	-X github.com/nuttakit/2-7-bot/internal/lapis.ResponseMinVisits=$(TOURMALINE_MIN_VISITS)

.PHONY: bot-tourmaline bot-tourmaline-release
bot-tourmaline:
	@test -f "$(TOURMALINE_POLICY)"
	@mkdir -p bin
	@overlay=$$(mktemp "$$(pwd)/bin/tourmaline-overlay.XXXXXX"); \
	  trap 'rm -f "$$overlay"' EXIT; \
	  $(AZURITE_OVERLAY) "$$overlay" "$(TOURMALINE_POLICY)" && \
	  go build -overlay "$$overlay" -ldflags='$(TOURMALINE_LDFLAGS)' -o $(TOURMALINE_NATIVE_PATH) ./cmd/bot

bot-tourmaline-release:
	@test -f "$(TOURMALINE_POLICY)"
	@mkdir -p bin
	@overlay=$$(mktemp "$$(pwd)/bin/tourmaline-overlay.XXXXXX"); \
	  trap 'rm -f "$$overlay"' EXIT; \
	  $(AZURITE_OVERLAY) "$$overlay" "$(TOURMALINE_POLICY)" && \
	  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -overlay "$$overlay" \
	    -ldflags='-s -w $(TOURMALINE_LDFLAGS)' -o bin/$(TOURMALINE_BOT_NAME) ./cmd/bot

# Spinel retains Tourmaline's bundled policy and adds a blocker-aware river
# solve using hero's held cards, private discards, and public action history.
SPINEL_POLICY ?= bin/tourmaline/selected.json
SPINEL_BOT_NAME ?= 27-spinel-6max-1
SPINEL_NATIVE_PATH ?= bin/bot-spinel-6max
SPINEL_PARTICLES ?= 512
SPINEL_LDFLAGS = -X main.playerProfile=river-response-blockers \
	-X github.com/nuttakit/2-7-bot/internal/lapis.Greedy=$(TOURMALINE_GREEDY) \
	-X github.com/nuttakit/2-7-bot/internal/lapis.ResponseMinVisits=$(TOURMALINE_MIN_VISITS) \
	-X github.com/nuttakit/2-7-bot/internal/lapis.BlockerParticles=$(SPINEL_PARTICLES)

.PHONY: bot-spinel bot-spinel-release
bot-spinel:
	@test -f "$(SPINEL_POLICY)"
	@mkdir -p bin
	@overlay=$$(mktemp "$$(pwd)/bin/spinel-overlay.XXXXXX"); \
	  trap 'rm -f "$$overlay"' EXIT; \
	  $(AZURITE_OVERLAY) "$$overlay" "$(SPINEL_POLICY)" && \
	  go build -overlay "$$overlay" -ldflags='$(SPINEL_LDFLAGS)' -o $(SPINEL_NATIVE_PATH) ./cmd/bot

bot-spinel-release:
	@test -f "$(SPINEL_POLICY)"
	@mkdir -p bin
	@overlay=$$(mktemp "$$(pwd)/bin/spinel-overlay.XXXXXX"); \
	  trap 'rm -f "$$overlay"' EXIT; \
	  $(AZURITE_OVERLAY) "$$overlay" "$(SPINEL_POLICY)" && \
	  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -overlay "$$overlay" \
	    -ldflags='-s -w $(SPINEL_LDFLAGS)' -o bin/$(SPINEL_BOT_NAME) ./cmd/bot

# The learned Spinel tracker is heads-up only today. At six seats it deliberately
# falls back to the unchanged Onyx strategy; this target tests that supported path.
# Duplicate dealing rotates Spinel through every seat, so HANDS decks are 6*HANDS hands.
spar-spinel-6max: engine bot-spinel
	$(ENGINE_BIN) run \
	  --game $(GAME) \
	  --hands $(HANDS) \
	  --dealing duplicate \
	  --bot 'spinel@cmd:$(SPINEL_NATIVE_PATH)' \
	  --bot 'random-1@builtin:random:1' \
	  --bot 'random-2@builtin:random:2' \
	  --bot 'random-3@builtin:random:3' \
	  --bot 'random-4@builtin:random:4' \
	  --bot 'random-5@builtin:random:5' \
	  --timeout-ms 1000 \
	  --fault-policy forfeit \
	  --output json

upload-spinel-6max-dry-run: arena bot-spinel-release
	API_KEY=offline-dry-run ./bin/arena upload \
	  --games $(GAME) \
	  --counts 6 \
	  --file bin/$(SPINEL_BOT_NAME) \
	  --dry-run

# Zircon is a native six-seat strategy with no heads-up model assets.
ZIRCON_BOT_NAME ?= 27-zircon-6max-2
ZIRCON_NATIVE_PATH ?= bin/bot-zircon
ZIRCON_PROFILE ?= generation2
ZIRCON_LDFLAGS = -X main.botProfile=$(ZIRCON_PROFILE)

.PHONY: bot-zircon bot-zircon-baseline bot-zircon-release spar-zircon-6max

bot-zircon:
	@mkdir -p bin
	go build -ldflags='$(ZIRCON_LDFLAGS)' -o $(ZIRCON_NATIVE_PATH) ./cmd/zircon

bot-zircon-baseline:
	@mkdir -p bin
	go build -ldflags='-X main.botProfile=baseline' -o bin/bot-zircon-v1 ./cmd/zircon

bot-zircon-release:
	@mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	  go build -trimpath -ldflags='-s -w $(ZIRCON_LDFLAGS)' -o bin/$(ZIRCON_BOT_NAME) ./cmd/zircon

# Duplicate dealing rotates Zircon through all six seats: HANDS decks produce
# 6*HANDS total hands.
spar-zircon-6max: engine bot-zircon
	$(ENGINE_BIN) run \
	  --game $(GAME) \
	  --hands $(HANDS) \
	  --dealing duplicate \
	  --bot 'zircon@cmd:$(ZIRCON_NATIVE_PATH)' \
	  --bot 'random-1@builtin:random:1' \
	  --bot 'random-2@builtin:random:2' \
	  --bot 'random-3@builtin:random:3' \
	  --bot 'random-4@builtin:random:4' \
	  --bot 'random-5@builtin:random:5' \
	  --timeout-ms 1000 \
	  --fault-policy forfeit \
	  --output json
