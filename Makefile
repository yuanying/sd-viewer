BIN := sd-viewer
WEB_DIST := internal/server/webui/dist

.PHONY: all build web test test-go test-web fmt vet dev clean clean-web

all: build

## build: 画面をビルドして単一バイナリを作る
build: web
	go build -o $(BIN) ./cmd/sd-viewer

## web: 画面をビルドして埋め込み先へ出力する
web: clean-web
	cd web && npm install --no-audit --no-fund && npm run build

## test: Go と画面のテストを実行する
test: test-go test-web

test-go:
	go test ./...

test-web:
	cd web && npm run test

fmt:
	gofmt -w .

vet:
	go vet ./...

## dev: 開発時は API サーバと画面を別々に起動する
dev:
	@echo "1) go run ./cmd/sd-viewer --dir <出力ディレクトリ>"
	@echo "2) cd web && npm run dev   # http://localhost:5173"

clean: clean-web
	rm -f $(BIN)

## clean-web: 埋め込み先を目印だけ残して空にする
clean-web:
	find $(WEB_DIST) -mindepth 1 ! -name .gitkeep -delete
