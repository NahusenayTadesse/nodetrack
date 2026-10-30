BIN     := nodetrack
LDFLAGS := -s -w
GOFLAGS := -trimpath

.PHONY: all ui build build-arm64 build-cgo test check dev clean distdir

all: ui build

ui:
	cd ui && npm ci && npm run build

# go:embed needs the directory to exist even before the UI is built.
distdir:
	@mkdir -p internal/web/dist && touch internal/web/dist/.keep

# Pure Go (default): static binary, cross-compiles anywhere.
build: distdir
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN) .

build-arm64: distdir
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN)-arm64 .

# C SQLite: ~half the binary size and ~1.5 MiB less RSS. Needs gcc, and
# should be built on (or for) the target's glibc.
build-cgo: distdir
	CGO_ENABLED=1 go build -tags cgo_sqlite $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN) .

test: distdir
	go vet ./... && go test ./...

check: test
	cd ui && npm run check

# Backend on :9100 plus Vite with hot reload on :5173 (proxies /api).
dev:
	go run . -db dev.db & cd ui && npm run dev

clean:
	rm -f $(BIN) $(BIN)-arm64 *.db *.db-wal *.db-shm
