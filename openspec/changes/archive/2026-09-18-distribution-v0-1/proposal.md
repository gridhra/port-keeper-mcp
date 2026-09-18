## Why

port-keeper-mcpを入れる手段は現在`go install`だけで、Goのツールチェーンを持たない人は使えない。gitタグもリリースも無い。機能（M0の台帳コア、M1のMCP）は完了しているので、残りは配布だけであり、これを済ませて初めて第三者が使える。

同じ作者の`gridhra/atx-mcp`（画像変換のMCPサーバー。手元では`../asset-transform-mcp`）のリリース構成と失敗の記録（`RELEASING.md`）を土台にする。ただしnpmラッパー（`npx`での起動）は採らない。理由は2つある。(1) port-keeperはMCPサーバーだけで完結せず、Claude Codeのフックと人の操作がPATH上の`port-keeper`コマンドを必要とするので、`npx`の都度起動ではなく常設インストールが向く。(2) atx-mcpではnpmの6パッケージの初回手動公開、パッケージごとの公開設定、リリースごとの承認など、メンテナの手作業が多かった。

## What Changes

- **公開前の実機確認**: 実際のClaude Codeセッションでフックと8つのMCPツールが動くこと、公式TypeScript SDKのクライアント（`mcp-proxy`）越しに`tools/list`が取れることを確かめる。これまでの確認はインプロセステストと生のstdio疎通だけである
- **リリースの自動化**: `v<major>.<minor>.<patch>`形式のタグをpushすると、GitHub Actionsの`release.yml`がGoReleaserで6種（darwin／linux／windows × amd64／arm64）のアーカイブとチェックサムの一覧を作り、ビルド来歴の証明（build provenance attestation）を付け、下書きのGitHub Releaseに全成果物を載せてから公開する。人の承認や手動公開の段は無い
- **インストールスクリプト**: `scripts/install.sh`（macOS／Linux）と`scripts/install.ps1`（Windows）。GitHub Releaseからその環境のアーカイブを取得し、チェックサムの照合を必須にして、利用者のホーム配下（既定`~/.local/bin`）に`port-keeper`を置く。`sudo`は使わない
- **台帳のスキーマの版**: 台帳（SQLite）にスキーマの版を記録し、自分より新しい版の台帳を開いたバイナリは、読み書きせずに更新を案内して終了する。現在は版の記録が無く、第三者の端末に台帳ができたあとでは入れにくいため、初回公開に含める
- **文書**: READMEのQuickstart（3言語）を「インストールスクリプト／手動ダウンロード／ソース」の3経路に書き換え、「Planned install channels」の段落を削る。メンテナ向けの`RELEASING.md`を新設する。`docs/ROADMAP.md`のM2を更新する
- **非目標の追加**: READMEの「Non-goals」に「コンテナイメージは配らない」を足す（3言語）。port-keeperはホストのネットワークでの`bind`確認、ホストの`lsof`、クライアントの作業ディレクトリ、ホーム配下の台帳を直接見るので、この4つを隔離するコンテナの中では機能が成立しない。公開後に来るであろう「Dockerイメージは無いのか」という要望に、利用者の目に入る場所で理由つきで答えておく
- **掲載**: Glama（MCPサーバーの登録・評価サイト）に登録し（提出前にローカルで同じ構成を検証する`scripts/glama.sh`を用意する）、awesome-mcp-serversに掲載を申請する。どちらもGitHub Releaseのバイナリだけで成立する
- **MCP公式レジストリ**: `v0.1.0`では登録しない。`server.json`からnpmの記述を除き、登録しない理由と再検討の条件を`docs/ROADMAP.md`に書く（理由はdesign.mdの「MCP公式レジストリ」の決定を参照）
- **前のchangeの始末**: `initial-implementation`の未完タスク4.3（Homebrew tap・npmラッパー・MCPレジストリ登録）を、npmラッパーは「採らない」、Homebrew tapとMCPレジストリは`docs/ROADMAP.md`のM3（需要駆動）へ移す、と書き換えて閉じ、`initial-implementation`をアーカイブする

範囲外（本changeでは作らない）: npmラッパー、Homebrew tap、MCP公式レジストリ登録、Windowsの実機確認、`doctor`のMCP設定検査。

## Capabilities

### New Capabilities
- `distribution`: タグからの再現可能なリリース、版の一致、インストールスクリプトの振る舞い、再実行の安全性、公式TypeScript SDKクライアントとの互換

### Modified Capabilities

- `port-ledger`: 台帳のスキーマの版の記録と、新しい版の台帳の拒否を要件として足す。既存の3要件は変えない。`port-ledger`の主specは`initial-implementation`のアーカイブ（本changeのタスクに含む）で生まれるので、本changeのアーカイブはその後に行う
- `mcp-tools`: 「失敗はツールのエラーとして返す（プロトコルエラーにしない）」を要件として足す。実装中に`render_env`で違反が見つかったため

## Impact

- 新規ファイル: `.github/workflows/release.yml`、`scripts/install.sh`、`scripts/install.ps1`、`scripts/glama.sh`、`RELEASING.md`
- 変更ファイル: `.goreleaser.yaml`、`server.json`、`README.md`／`README.ja.md`／`README.zh-CN.md`、`docs/ROADMAP.md`、`docs/DESIGN.md`（§9実装ノート）、`CLAUDE.md`（リリース手順への入口）、`.github/workflows/ci.yml`（インストールスクリプトの検査）
- Goのコード: `internal/ledger`（スキーマの版）。ほかは、実機確認で不具合が見つかった場合だけ直す
- 外部: GitHub Release、Glama、awesome-mcp-serversへのPR。GitHubのリポジトリ設定（Immutable releases）
- 人の作業が要る箇所: Glamaへのログインと所有の申告、awesome-mcp-servers PRの送信判断、`main`とタグのpush。リリースごとの承認作業は無い
