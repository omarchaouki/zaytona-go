module github.com/omarchaouki/zaytona-go

go 1.27.0

toolchain go1.27.2

require (
	github.com/a-h/templ v0.3.1100
	github.com/altcha-org/altcha-lib-go/v2 v2.4.1
	github.com/chromedp/cdproto v0.158.6
	github.com/chromedp/chromedp v0.21.0
	github.com/evanw/esbuild v0.28.2
	github.com/fergusstrange/embedded-postgres v1.34.0
	github.com/gen2brain/avif v0.6.0
	github.com/gen2brain/webp v0.6.4
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx-shopspring-decimal v0.0.0-20220624020537-1d36b5a1853e
	github.com/jackc/pgx/v5 v5.11.0
	github.com/microcosm-cc/bluemonday v1.0.27
	github.com/pquerna/otp v1.5.0
	github.com/pressly/goose/v3 v3.29.0
	github.com/shopspring/decimal v1.6.0
	github.com/wneessen/go-mail v0.8.1
	golang.org/x/crypto v0.58.0
	golang.org/x/image v0.47.0
	golang.org/x/text v0.43.0
)

require (
	cel.dev/expr v0.25.2 // indirect
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/a-h/parse v0.0.0-20250122154542-74294addb73e // indirect
	github.com/andybalholm/brotli v1.2.7 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/boombuler/barcode v1.0.1-0.20190219062509-6c824513bacc // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cli/browser v1.3.0 // indirect
	github.com/coreos/go-semver v0.3.1 // indirect
	github.com/cubicdaiya/gonp v1.0.4 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/ebitengine/purego v0.10.1 // indirect
	github.com/fatih/color v1.19.0 // indirect
	github.com/fatih/structtag v1.2.0 // indirect
	github.com/fsnotify/fsnotify v1.10.1 // indirect
	github.com/go-json-experiment/json v0.0.0-20260213210345-44df1a37e875 // indirect
	github.com/go-sql-driver/mysql v1.10.1 // indirect
	github.com/google/cel-go v0.28.0 // indirect
	github.com/gorilla/css v1.0.1 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.3 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/lib/pq v1.12.3 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/natefinch/atomic v1.0.1 // indirect
	github.com/ncruces/go-sqlite3 v0.32.0 // indirect
	github.com/ncruces/julianday v1.0.0 // indirect
	github.com/pganalyze/pg_query_go/v6 v6.2.2 // indirect
	github.com/pingcap/errors v0.11.5-0.20250523034308-74f78ae071ee // indirect
	github.com/pingcap/failpoint v0.0.0-20240528011301-b51a646c7c86 // indirect
	github.com/pingcap/log v1.1.0 // indirect
	github.com/pingcap/tidb/pkg/parser v0.0.0-20260418072757-ce92298d1124 // indirect
	github.com/riza-io/grpc-go v0.2.0 // indirect
	github.com/sethvargo/go-retry v0.5.0 // indirect
	github.com/spf13/cobra v1.10.2 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/sqlc-dev/doubleclick v1.0.0 // indirect
	github.com/sqlc-dev/sqlc v1.31.1 // indirect
	github.com/tetratelabs/wazero v1.12.0 // indirect
	github.com/wasilibs/go-pgquery v0.0.0-20250409022910-10ac41983c07 // indirect
	github.com/wasilibs/wazero-helpers v0.0.0-20240620070341-3dff1577cd52 // indirect
	github.com/xi2/xz v0.0.0-20171230120015-48954b6210f8 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/exp v0.0.0-20261011100332-faecfb24eb49 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/telemetry v0.0.0-20261010193011-f71dc92da45e // indirect
	golang.org/x/tools v0.52.0 // indirect
	golang.org/x/vuln v1.9.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20261005182115-fad411399dd8 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261005182115-fad411399dd8 // indirect
	google.golang.org/grpc v1.84.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	mvdan.cc/gofumpt v0.12.0 // indirect
)

tool (
	github.com/a-h/templ/cmd/templ
	github.com/sqlc-dev/sqlc/cmd/sqlc
	golang.org/x/vuln/cmd/govulncheck
	mvdan.cc/gofumpt
)
