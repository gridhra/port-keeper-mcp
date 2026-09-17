## 1. 台帳コア（M0）

- [x] 1.1 config（プール既定20000〜31999、deny、XDGパス）
- [x] 1.2 manifest（`port-keeper.toml`の解析・検証・雛形）
- [x] 1.3 ledger（スキーマ、`port UNIQUE`、immediateトランザクション、0700/0600）
- [x] 1.4 ブロック割当（first-fit、既存ブロック・リース・deny・実在確認を回避）
- [x] 1.5 probe（bind＋connect、lsofによる所有者）
- [x] 1.6 app（解決、同期、インフラ共有、解放、gc候補、pin/unpin、reassign、状態）
- [x] 1.7 render（dotenv/export/json/mise/direnv/claude-env、テンプレート、マーカー冪等更新）
- [x] 1.8 CLI（init/slot/env/url/status/gc/doctor/pin/unpin/reassign/mcp/version）
- [x] 1.9 テスト（割当不変条件・並行・UNIQUE・描画ゴールデン・マニフェスト検証・probe・固定調停・reassign・追跡ファイル拒否）
- [x] 1.10 実プロジェクト相当（26サービス・6スロット・既存番号の固定）での実機テストと、その一般化した自動テスト（`internal/app/migration_test.go`、`internal/cli/cli_test.go`）
- [x] 1.11 移行の手触り改善（`pin`の複数指定、`unpin --all`、`slot ls --pins`、紐づけの保護）
- [x] 1.12 独立レビュー対応（未紐づけworktreeの拒否、解決順の変更、Windowsビルド、MCPの`cwd`引数、孤児スロットの解放、原子的書き込み、`lsof`集約ほか。DESIGN §9.1参照）

## 2. MCP（M1）

- [x] 2.1 stdioサーバーと8ツール、annotations、structuredContent
- [x] 2.2 `slot_release`／`list_all_projects`は設定で有効化した場合のみ登録
- [x] 2.3 インプロセスの統合テスト（ツール面・開示・ドライラン）
- [x] 2.4 stdioでの手動疎通（initialize → tools/call → tools/list）

## 3. 文書

- [x] 3.1 README（Non-goals、Working with coding agents、Security model）
- [x] 3.2 SECURITY.md、issueテンプレート、docs/DESIGN.md §9実装ノート、docs/ROADMAP.md
- [x] 3.3 README.ja.md・README.zh-CN.md

## 4. 配布（M2）

- [x] 4.1 CI（gofmt/vet/test）
- [x] 4.2 GoReleaser設定（未実行）
- [x] 4.3 Homebrew tap・npmラッパー・MCPレジストリ登録 → 本changeでは行わず、change`distribution-v0-1`で結論を出した（2026-09-18）。npmラッパーは作らない（常設インストールに変更）。Homebrew tapとMCPレジストリ登録は`docs/ROADMAP.md`のM3（需要駆動）へ移した。理由は`docs/DESIGN.md`の§9.2
