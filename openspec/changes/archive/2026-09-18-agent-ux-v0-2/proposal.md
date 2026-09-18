## Why

`v0.1.0`を公開して試験用プロジェクトで使い始めると、人とコーディングエージェントが次に躓く点が見えてきた。(1) マニフェストにサービスを足したり`reassign`したりしても、書き出した`.env.local`が古いままであることに誰も気づけない（`render_env`は台帳を更新するが、ファイルは`port-keeper env`を打つまで変わらない）。(2) 移行したプロジェクトで、git追跡下の`.env`に`WEB_PORT=3000`が残っていると、dotenvローダーによっては`.env.local`より勝ち、「port-keeperを入れたのに古い番号で動く」事故になるが、検出する手段が無い。(3) 設計書§5.5で約束した「MCP設定ファイルに番号やトークンが無いか」の`doctor`検査が未実装。(4) `status`に機械可読形が無く、スクリプトや他クライアントのアダプタが読めない。(5) 人がスロット名・サービス名を打つときの補完が無い。(6) mise／direnv／docker compose／Vite／Playwright／プロキシとのつなぎ方が文書に無く、導入で止まる。(7) ROADMAPの「Agent UXの規律」5で約束したエージェントevalが無く、ツール説明や文言の変更の回帰を目視でしか確かめられない。(8) README 3言語の鏡写しが崩れても気づけない。

これらをまとめて`v0.2.0`として出す。

## What Changes

- **書き出した環境の古さ（drift）検出**: `.env.local`のマーカーブロックを台帳から描画し直した結果と突き合わせ、違えば名前だけの理由（「service mail has no port yet」「.env.local is out of date」）を返す`App.EnvDrift`。`context --json`に`env_stale`／`env_stale_reason`、案内文の末尾に`port-keeper env`の指示。Claude Codeフック（SessionStartとCwdChanged）と`doctor`も同じ理由を出す。台帳のスキーマは変えない
- **`doctor`の追加検査**: MCPクライアント設定（Claude Code、Claude Desktop、Cursor、VS Code、Codex）のport-keeper項目に、ポートらしい番号やトークンらしい環境変数が無いか。追跡済みの`.env`／`.env.*`が管理対象の変数を固定していないか。どちらも**値を出力しない**。`[fail]`があれば終了コード1
- **`status --json`**: CLIの`status`の機械可読形（番号を含む。MCPの`status`ツールは変えない）
- **`slot new --from-branch`**: gitブランチ名を正規化してスロット名にする
- **`completion zsh|bash|fish`**: 補完スクリプト。動的候補は隠しコマンド`__complete`から。番号は出さない
- **`docs/examples/`**: mise、direnv、docker compose、Vite、Playwright、リバースプロキシ（localias／portless）、monorepoの8ファイル。英語のみ
- **README同期検査**: `scripts/readme_sync_check.sh`（見出し数・コードブロック数・表の行の並び）をCIの`test`ジョブに
- **エージェントeval**: `scripts/agent_eval.sh`。MCP接続済みのClaude Codeに3つの作業をさせ、往復数と番号の露出を数える。APIを呼ぶので手動のリリースゲート。`--list`と`--dry-run`だけCI
- **文書**: README 3言語のCLI表など、`docs/DESIGN.md` §9.3、`docs/ROADMAP.md`（M2.5）、`RELEASING.md`（evalの手順）、`SECURITY.md`（設定ファイルを読むことを範囲に）、`CLAUDE.md`

範囲外: 1サービスに複数ポート（要望未確認）、Codex CLI／Cursor向けフックアダプタ（フック機構が未調査）、Windows実機確認、MCPツールの追加（8個のまま）。

## Capabilities

### New Capabilities
- `doctor`: 自己点検の範囲（既存の検査を書き下し、MCP設定検査と追跡済みdotenv検査とdriftを足す）、値を出さない規律、終了コード
- `shell-completion`: 補完スクリプトと隠し`__complete`の振る舞い

### Modified Capabilities
- `env-rendering`: driftの定義と表に出る場所
- `slot-allocation`: `--from-branch`の正規化規則
- `port-ledger`: `status --json`
- `distribution`: README同期検査、エージェントeval、`docs/examples/`は英語のみ

## Impact

- 新規: `internal/doctor/`、`internal/cli/completion.go`、`scripts/readme_sync_check.sh`、`scripts/agent_eval.sh`、`docs/examples/`（8ファイル）
- 変更: `internal/app/app.go`（`EnvDrift`、`SlotNameFromBranch`）、`internal/cli/cli.go`（`status --json`、`doctor`、`context`、`completion`）、`internal/cli/hook_claude.go`、`internal/gitx/gitx.go`（`Branch`、`TrackedFiles`）、`internal/render/render.go`（`DotenvBlock`）、`.github/workflows/ci.yml`、README 3言語、`docs/`、`RELEASING.md`、`SECURITY.md`、`CLAUDE.md`
- 外部: 依存の追加なし。台帳スキーマの版は1のまま
- 人の手が要る段: 本番のエージェントeval（APIキー）、`v0.2.0`のタグpush
