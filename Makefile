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

.PHONY: help arena bot bot-release bot-model-release bot-model test fmt vet docs-check engine spar sixmax sixmax-release sixmax-spar sixmax-spar-mixed sixmax-fit sixmax-clone-fit sixmax-h3 sixmax-h3-release sixmax-h3-spar-mixed sixmax-h4-aggr sixmax-h4-aggr-release sixmax-h4-both sixmax-h4-both-release sixmax-h5 sixmax-h5-release sixmax-h5b-fit sixmax-h5b sixmax-h5b-release

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
	@echo 'sixmax     build the native six-player bot into bin/bot-sixmax'
	@echo 'sixmax-release build the static Linux six-player upload artifact'

arena:
	go build -o bin/arena ./cmd/arena

# For sparring: a host-native build, so the local engine can spawn it.
bot:
	go build -ldflags='$(ONYX_LDFLAGS)' -o bin/bot ./cmd/bot

# The upload artifact. One static Linux x86-64 ELF, named for the bot
# (docs/naming.md) because `arena upload --name` defaults to the filename.
BOT_NAME ?= 2-7-onyx-10

bot-release:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	  go build -trimpath -ldflags='-s -w $(ONYX_LDFLAGS)' -o bin/$(BOT_NAME) ./cmd/bot

# Generated assets stay in the ignored bin directory. The overlay substitutes
# them only for this build and leaves the baseline embedded assets untouched.
MODEL_POLICY ?= bin/onyx-swit-policy-history-large.json.gz
MODEL_BLUEPRINT ?= bin/onyx-78-blueprint.bin.gz
MODEL_BOT_NAME ?= 2-7-onyx-158
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

SIXMAX_BOT_NAME ?= nutt-27td-fl-6max-h2
SIXMAX_NATIVE_PATH ?= bin/bot-sixmax
SIXMAX_HANDS ?= 1000
SIXMAX_SEED ?= 270601
SIXMAX_H3_NAME ?= nutt-27td-fl-6max-h3
SIXMAX_H3_NATIVE_PATH ?= bin/bot-sixmax-h3
SIXMAX_RANGE_MODEL ?= bin/sixmax/range-h3.json
SIXMAX_RANGE_SUMMARY ?= bin/sixmax/range-h3-summary.json
SIXMAX_RANGE_MATCHES ?= 36,37,38,39,41,85,101,102,103
SIXMAX_CLONE_MATCHES ?= 36,37,38,39,41,77,84,85,101,102,1081,1086
SIXMAX_CLONE_MODEL ?= bin/sixmax/models/clone-predraw-h5.json
SIXMAX_CLONE_METRICS ?= bin/sixmax/models/clone-predraw-h5-metrics.json
SIXMAX_H5_NATIVE_PATH ?= bin/bot-sixmax-h5
SIXMAX_H5_NAME ?= nutt-27td-fl-6max-h5
SIXMAX_H5B_MODEL ?= bin/sixmax/models/clone-predraw-h5b.json
SIXMAX_H5B_METRICS ?= bin/sixmax/models/clone-predraw-h5b-metrics.json
SIXMAX_H5B_SOURCES ?= bin/sixmax/models/clone-predraw-h5b-sources.json
SIXMAX_H5B_NATIVE_PATH ?= bin/bot-sixmax-h5b
SIXMAX_H5B_NAME ?= nutt-27td-fl-6max-h5b
SIXMAX_H5B_TRAIN_MATCHES ?= 36,37,38,39,41,77,84,85,101,102,1081,1086
SIXMAX_H5B_TRAIN ?= bin/sixmax/clone-data/pilot/match-1094,bin/sixmax/clone-data/pilot/match-1095,bin/sixmax/clone-data/pilot/match-1096,bin/sixmax/clone-data/train/match-1097,bin/sixmax/clone-data/train/match-1098,bin/sixmax/clone-data/train/match-1099,bin/sixmax/clone-data/train/match-1100,bin/sixmax/clone-data/train/match-1101,bin/sixmax/clone-data/train/match-1102,bin/sixmax/clone-data/train/match-1103,bin/sixmax/clone-data/train/match-1104,bin/sixmax/clone-data/train/match-1105,bin/sixmax/clone-data/train/match-1109,bin/sixmax/clone-data/train/match-1110,bin/sixmax/clone-data/train/match-1111,bin/sixmax/clone-data/train/match-1115,bin/sixmax/clone-data/train/match-1116,bin/sixmax/clone-data/train/match-1117,bin/sixmax/clone-data/train/match-1118,bin/sixmax/clone-data/train/match-1119,bin/sixmax/clone-data/train/match-1120,bin/sixmax/clone-data/train/match-1121
SIXMAX_H5B_VALIDATION ?= bin/sixmax/clone-data/validation/match-1112,bin/sixmax/clone-data/validation/match-1113,bin/sixmax/clone-data/validation/match-1114
SIXMAX_RANGE_OVERLAY = python3 -c 'import json,pathlib,sys; r=pathlib.Path.cwd(); pathlib.Path(sys.argv[1]).write_text(json.dumps({"Replace":{str(r/"internal/sixmaxrange/default_model.json"):str(pathlib.Path(sys.argv[2]).resolve())}}))'
SIXMAX_CLONE_OVERLAY = python3 -c 'import json,pathlib,sys; r=pathlib.Path.cwd(); pathlib.Path(sys.argv[1]).write_text(json.dumps({"Replace":{str(r/"internal/sixmaxclone/default_model.json"):str(pathlib.Path(sys.argv[2]).resolve())}}))'
SIXMAX_VALUE_LDFLAGS = -X main.valueAggression=true -X main.rangeCalls=false
SIXMAX_BOTH_LDFLAGS = $(SIXMAX_VALUE_LDFLAGS) -X main.earlyTenBreak=true

sixmax:
	@mkdir -p bin
	go build -o $(SIXMAX_NATIVE_PATH) ./cmd/sixmax

sixmax-release:
	@mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	  go build -trimpath -ldflags='-s -w' -o bin/$(SIXMAX_BOT_NAME) ./cmd/sixmax

sixmax-fit:
	go run ./cmd/sixmaxfit -data bin/sixmax/data -matches $(SIXMAX_RANGE_MATCHES) \
	  -holdout 103 -out $(SIXMAX_RANGE_MODEL) -summary $(SIXMAX_RANGE_SUMMARY)

sixmax-clone-fit:
	go run ./cmd/sixmaxclonefit -data bin/sixmax/data -matches $(SIXMAX_CLONE_MATCHES) \
	  -out $(SIXMAX_CLONE_MODEL) -metrics $(SIXMAX_CLONE_METRICS)

sixmax-h5: sixmax-clone-fit
	@overlay=$$(mktemp "$$(pwd)/bin/sixmax-h5-overlay.XXXXXX"); \
	  trap 'rm -f "$$overlay"' EXIT; \
	  $(SIXMAX_CLONE_OVERLAY) "$$overlay" "$(SIXMAX_CLONE_MODEL)" && \
	  go build -overlay "$$overlay" -ldflags='-X main.clonePredraw=true' -o $(SIXMAX_H5_NATIVE_PATH) ./cmd/sixmax

sixmax-h5-release: sixmax-clone-fit
	@overlay=$$(mktemp "$$(pwd)/bin/sixmax-h5-overlay.XXXXXX"); \
	  trap 'rm -f "$$overlay"' EXIT; \
	  $(SIXMAX_CLONE_OVERLAY) "$$overlay" "$(SIXMAX_CLONE_MODEL)" && \
	  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -overlay "$$overlay" \
	    -ldflags='-s -w -X main.clonePredraw=true' -o bin/$(SIXMAX_H5_NAME) ./cmd/sixmax

sixmax-h5b-fit:
	go run ./cmd/sixmaxclonefit -data bin/sixmax/data -training-matches $(SIXMAX_H5B_TRAIN_MATCHES) \
	  -train-dirs $(SIXMAX_H5B_TRAIN) -validation-dirs $(SIXMAX_H5B_VALIDATION) \
	  -out $(SIXMAX_H5B_MODEL) -metrics $(SIXMAX_H5B_METRICS) -source-snapshot $(SIXMAX_H5B_SOURCES)

sixmax-h5b: sixmax-h5b-fit
	@overlay=$$(mktemp "$$(pwd)/bin/sixmax-h5b-overlay.XXXXXX"); \
	  trap 'rm -f "$$overlay"' EXIT; \
	  $(SIXMAX_CLONE_OVERLAY) "$$overlay" "$(SIXMAX_H5B_MODEL)" && \
	  go build -overlay "$$overlay" -ldflags='-X main.clonePredraw=true' -o $(SIXMAX_H5B_NATIVE_PATH) ./cmd/sixmax

sixmax-h5b-release: sixmax-h5b-fit
	@overlay=$$(mktemp "$$(pwd)/bin/sixmax-h5b-overlay.XXXXXX"); \
	  trap 'rm -f "$$overlay"' EXIT; \
	  $(SIXMAX_CLONE_OVERLAY) "$$overlay" "$(SIXMAX_H5B_MODEL)" && \
	  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -overlay "$$overlay" \
	    -ldflags='-s -w -X main.clonePredraw=true' -o bin/$(SIXMAX_H5B_NAME) ./cmd/sixmax

sixmax-h3: sixmax-fit
	@overlay=$$(mktemp "$$(pwd)/bin/sixmax-h3-overlay.XXXXXX"); \
	  trap 'rm -f "$$overlay"' EXIT; \
	  $(SIXMAX_RANGE_OVERLAY) "$$overlay" "$(SIXMAX_RANGE_MODEL)" && \
	  go build -overlay "$$overlay" -ldflags='-X main.rangeCalls=true' -o $(SIXMAX_H3_NATIVE_PATH) ./cmd/sixmax

sixmax-h3-release: sixmax-fit
	@overlay=$$(mktemp "$$(pwd)/bin/sixmax-h3-overlay.XXXXXX"); \
	  trap 'rm -f "$$overlay"' EXIT; \
	  $(SIXMAX_RANGE_OVERLAY) "$$overlay" "$(SIXMAX_RANGE_MODEL)" && \
	  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -overlay "$$overlay" \
	    -ldflags='-s -w -X main.rangeCalls=true' -o bin/$(SIXMAX_H3_NAME) ./cmd/sixmax

sixmax-h4-aggr:
	go build -ldflags='$(SIXMAX_VALUE_LDFLAGS)' -o bin/bot-sixmax-h4-aggr ./cmd/sixmax

sixmax-h4-aggr-release:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
	  -ldflags='-s -w $(SIXMAX_VALUE_LDFLAGS)' -o bin/nutt-27td-fl-6max-h4-aggr ./cmd/sixmax

sixmax-h4-both:
	go build -ldflags='$(SIXMAX_BOTH_LDFLAGS)' -o bin/bot-sixmax-h4-both ./cmd/sixmax

sixmax-h4-both-release:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
	  -ldflags='-s -w $(SIXMAX_BOTH_LDFLAGS)' -o bin/nutt-27td-fl-6max-h4-both ./cmd/sixmax

sixmax-spar: engine sixmax
	$(ENGINE_BIN) run --game 27td-fl --hands $(SIXMAX_HANDS) --seed $(SIXMAX_SEED) \
	  --fault-policy forfeit --timeout-ms 1000 --output json \
	  --log bin/sixmax-callers-seed$(SIXMAX_SEED).jsonl --log-sample 6 --log-top 12 \
	  --bot 'candidate@cmd:$(SIXMAX_NATIVE_PATH)' \
	  --bot 'caller-1@builtin:caller' --bot 'caller-2@builtin:caller' \
	  --bot 'caller-3@builtin:caller' --bot 'caller-4@builtin:caller' \
	  --bot 'caller-5@builtin:caller' \
	  > bin/sixmax-callers-seed$(SIXMAX_SEED).json

sixmax-spar-mixed: engine sixmax
	$(ENGINE_BIN) run --game 27td-fl --hands $(SIXMAX_HANDS) --seed $(SIXMAX_SEED) \
	  --fault-policy forfeit --timeout-ms 1000 --output json \
	  --log bin/sixmax-mixed-seed$(SIXMAX_SEED).jsonl --log-sample 6 --log-top 12 \
	  --bot 'candidate@cmd:$(SIXMAX_NATIVE_PATH)' \
	  --bot 'caller-1@builtin:caller' --bot 'caller-2@builtin:caller' \
	  --bot 'random-1@builtin:random:1' --bot 'random-2@builtin:random:2' \
	  --bot 'shover@builtin:shover' \
	  > bin/sixmax-mixed-seed$(SIXMAX_SEED).json

sixmax-h3-spar-mixed: engine sixmax-h3
	$(ENGINE_BIN) run --game 27td-fl --hands $(SIXMAX_HANDS) --seed $(SIXMAX_SEED) \
	  --fault-policy forfeit --timeout-ms 1000 --output json \
	  --log bin/sixmax-h3-mixed-seed$(SIXMAX_SEED).jsonl --log-sample 6 --log-top 12 \
	  --bot 'candidate@cmd:$(SIXMAX_H3_NATIVE_PATH)' \
	  --bot 'caller-1@builtin:caller' --bot 'caller-2@builtin:caller' \
	  --bot 'random-1@builtin:random:1' --bot 'random-2@builtin:random:2' \
	  --bot 'shover@builtin:shover' \
	  > bin/sixmax-h3-mixed-seed$(SIXMAX_SEED).json

# Azurite: the equity-bucketed self-play blueprint, selected by local
# exploitability (cfrgen exploit) rather than hosted matches. The blueprint
# is too large to commit; it stays in bin/ and is overlaid at build time,
# so the build flags here must match the ones it was trained with.
AZURITE_BUCKETS ?= 160,160,160
AZURITE_FIXED ?= none
AZURITE_BLUEPRINT ?= bin/azurite.bin.gz
AZURITE_BOT_NAME ?= 2-7-azurite-1
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
OBSIDIAN_BOT_NAME ?= 2-7-obsidian-1
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
TOURMALINE_BOT_NAME ?= 2-7-tourmaline-1
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
