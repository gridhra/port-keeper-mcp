# Spec Delta

## Purpose

port-keeper-mcpを、Goのツールチェーンを持たない第三者が`port-keeper`コマンドとして常設インストールでき、配布物がどのソースから作られたかを検証できる形で配布する。リリースはタグのpushだけで完結し、失敗したリリースを安全にやり直せることを保証する。

## ADDED Requirements

### Requirement: タグからのリリース
リリースの仕組みは、`v<major>.<minor>.<patch>`形式のgitタグがpushされたとき、そのタグのソースからdarwin／linux／windowsのamd64／arm64（計6種）のアーカイブ、チェックサムの一覧、ビルド来歴の証明を作り、人の操作を挟まずにGitHub Releaseとして公開しなければならない（SHALL）。形式に合わないタグでは何も公開してはならない（MUST NOT）。

#### Scenario: 正しい形式のタグ
- **WHEN** メンテナが`v0.1.0`をpushする
- **THEN** GitHub Release `v0.1.0`に6種のアーカイブとチェックサムの一覧が載り、各アーカイブに`LICENSE`と`README.md`が同梱され、`gh attestation verify <アーカイブ> -R gridhra/port-keeper-mcp`が成功する

#### Scenario: 形式に合わないタグ
- **WHEN** `v0.1`や`v0.1.0; rm -rf /`のような、形式に合わない名前のタグでワークフローが起動する
- **THEN** ワークフローは最初のステップで失敗し、GitHub Releaseは作られない

### Requirement: 版の一致
配布物の版は、タグから先頭の`v`を除いた値に一致しなければならない（MUST）。対象は`port-keeper --version`の出力と、MCPの`initialize`応答の`serverInfo.version`である。

#### Scenario: 公開されたバイナリの版
- **WHEN** `v0.1.0`のGitHub Releaseから取得したバイナリで`port-keeper --version`を実行し、`port-keeper mcp`に`initialize`を送る
- **THEN** 前者の出力が`0.1.0`を含み、後者の`serverInfo.version`が`0.1.0`である

### Requirement: インストールスクリプト
インストールスクリプトは、実行環境のOSとCPUに合うアーカイブをGitHub Releaseから取得し、チェックサムの一覧と照合してから、利用者のホーム配下に`port-keeper`を置かなければならない（SHALL）。照合に失敗したとき、または照合の手段が無いときは、何も置かずに失敗しなければならない（MUST）。管理者権限を要求してはならない（MUST NOT）。

#### Scenario: 既定のインストール
- **WHEN** macOSまたはLinuxで、READMEに書かれた1行のコマンドでスクリプトを実行する
- **THEN** 最新のリリースの`port-keeper`が`~/.local/bin`に置かれ、置いた場所と版が表示され、その場所がPATHに無いときはPATHへの足し方が表示される

#### Scenario: 版と置き場所の指定
- **WHEN** 版（例: `0.1.0`）と置き場所を指定して実行する
- **THEN** 指定した版が指定した場所に置かれる

#### Scenario: チェックサムの不一致
- **WHEN** 取得したアーカイブのSHA256がチェックサムの一覧と一致しない
- **THEN** スクリプトは0以外で終了し、置き場所には何も書かれず、既存の`port-keeper`があれば元のまま残る

#### Scenario: 対応していない環境
- **WHEN** 配布していないOS／CPUの組み合わせで実行する
- **THEN** 検出した環境の名前と、`go install`での導入方法が表示され、0以外で終了する

#### Scenario: 上書き更新
- **WHEN** すでに`port-keeper`がある置き場所に対して再実行する
- **THEN** バイナリが新しい版に置き換わり、台帳と設定ファイルには触れない

### Requirement: コンテナイメージを配布しない
プロジェクトは、port-keeperを実行するためのコンテナイメージを配布してはならず（MUST NOT）、コンテナからの起動を導入経路として案内してはならない（MUST NOT）。READMEの非目標（Non-goals）の節は、その理由として、コンテナが隔離する4つのもの（ホストのネットワーク、ホストのプロセス、クライアントの作業ディレクトリ、ホーム配下の台帳）をport-keeperが直接必要とすることを述べなければならない（SHALL）。登録サイトが動作確認のために一時的に作るコンテナ（Glamaのチェック）は、配布にあたらない。

#### Scenario: 利用者がコンテナでの実行方法を探す
- **WHEN** 利用者がREADME（英語・日本語・中国語のいずれか）でコンテナやDockerでの導入方法を探す
- **THEN** 非目標の節に「コンテナイメージは配らない」の項があり、上の4つの理由と、代わりの導入経路（インストールスクリプト）が書かれている

#### Scenario: リリースの成果物
- **WHEN** リリースのワークフローが完了する
- **THEN** どのコンテナレジストリにもイメージは公開されず、`server.json`に`oci`形式のパッケージは無い

### Requirement: 再実行の安全性
リリースのワークフローは、途中で失敗したあとに同じタグで再実行しても、公開済みのGitHub Releaseの成果物を書き換えてはならない（MUST NOT）。

#### Scenario: 公開前の失敗からの再実行
- **WHEN** 下書きのGitHub Releaseが残っている状態で、同じタグで再実行する
- **THEN** 下書きは作り直され、全成果物が揃ってから公開される

#### Scenario: 公開済みのタグでの再実行
- **WHEN** 公開済み（下書きでない）のGitHub Releaseがあるタグで再実行する
- **THEN** ワークフローは成果物に触れずに、公開済みであることを示して終了する

### Requirement: 公式TypeScript SDKクライアントとの互換
MCPサーバーは、公式TypeScript SDKで作られたクライアント（`mcp-proxy`を含む）からの`initialize`と`tools/list`に、プロジェクトの外の作業ディレクトリで起動された場合でも成功で応答しなければならない（SHALL）。この確認は、Glamaへ提出する前にローカルで再現できなければならない（MUST）。

#### Scenario: mcp-proxy越しの一覧取得
- **WHEN** Glamaが生成するのと同じ構成（`debian:trixie-slim`とNodeと`mcp-proxy`）のコンテナで、GitHub Releaseから取得したlinux-amd64のバイナリを`mcp-proxy`越しに起動し、`initialize`と`tools/list`を送る
- **THEN** `serverInfo.version`がその版で、既定で有効な6ツールの一覧が返り、スキーマ検証のエラーで拒否されない

#### Scenario: プロジェクトの外での起動
- **WHEN** `port-keeper.toml`が無いディレクトリで`port-keeper mcp`を起動する
- **THEN** サーバーは起動して`tools/list`に応答し、プロジェクトを必要とするツールの呼び出しだけが、作り方を案内するエラーを返す
