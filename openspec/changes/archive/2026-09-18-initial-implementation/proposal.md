## Why

ローカル開発のポート番号は「基準値＋スロット×1000」のような導出式と人の記憶で管理されており、サービス間の基準値差がスロット幅の整数倍になると衝突して環境数に上限が生まれ、プロジェクト横断の衝突は誰も裁いていない。人もAIエージェントも番号を覚える必要があり、番号をチャットや文書に貼る漏えい経路にもなっている。

## What Changes

- ローカルの台帳（SQLite 1ファイル、0600）にポートのリースを記録し、`project/slot/service`の名前で引けるCLI `port-keeper` を新設する
- プロジェクトがcommitするマニフェスト `port-keeper.toml`（サービス名・環境変数名・導出式のみ。番号は書かない）を定義する
- スロットごとに連続ブロックをプールから貸し出し、`.env`ブロック／shell export／JSON／mise／direnv／Claude Codeの`CLAUDE_ENV_FILE`向けに描画する
- 同じバイナリの`mcp`サブコマンドでstdio MCPサーバーを提供し、既定6ツール＋設定で有効化する2ツールを公開する
- 互換のための固定番号（pin）を、全プロジェクト横断の調停付き例外として許す
- 常駐デーモン・名前解決プロキシ・プロセス管理は作らない（非目標）

## Capabilities

### New Capabilities
- `port-ledger`: リースの永続化、重複割当の禁止、実在確認と状態（leased/active/stale/hijacked）
- `slot-allocation`: マニフェストからのスロット作成、連続ブロック割当、プール外・慣習的番号の回避、インフラ共有
- `env-rendering`: 描画形式、テンプレート変数、dotenvマーカーブロックの冪等更新、追跡済みファイルの拒否
- `mcp-tools`: MCPツール面（8ツール）、annotations、番号を返すツールの限定、他プロジェクト参照の明示要求
- `pin-arbitration`: 固定番号の横断調停、理由必須、既定スロット限定、解除
- `ledger-confidentiality`: 台帳の権限、保持しない情報、待受なし、開示の最小化

### Modified Capabilities

（なし。初回実装）

## Impact

- 新規Goモジュール `github.com/gridhra/port-keeper-mcp`（`cmd/port-keeper`、`internal/{config,manifest,ledger,probe,gitx,render,app,cli,mcpserver}`）
- 依存: `modernc.org/sqlite`（cgo不要）、`github.com/BurntSushi/toml`、`github.com/modelcontextprotocol/go-sdk`
- 利用者の端末に `~/.local/state/port-keeper/ledger.sqlite` と `~/.config/port-keeper/config.toml`（任意）が生まれる
