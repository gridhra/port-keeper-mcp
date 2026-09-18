## Context

動機はproposal.mdの「Why」を参照。設計を縛る現状は次のとおり。

- バイナリは`cmd/port-keeper`の1本で、cgo不要（`modernc.org/sqlite`）。1台のLinuxランナーから6種すべてをクロスコンパイルできる。版は`-X github.com/gridhra/port-keeper-mcp/internal/cli.Version=...`で埋め込まれ、`--version`とMCPの`serverInfo.version`の両方に使われる
- `.goreleaser.yaml`は未実行の雛形（`release.draft: true`、チェックサムは`checksums.txt`）
- `main`は保護されていて、`v*`タグの保護ルールセットも設定済み（`CLAUDE.local.md`の記録。GitHub上での再確認は本changeのタスク）。GitHub Actionsのアクションはcommit SHAで固定し、Dependabotが更新する方針
- 台帳は開くたびに`CREATE TABLE IF NOT EXISTS`を流すだけで、スキーマの版を持たない（`internal/ledger/ledger.go`）
- 参考にするatx-mcp（手元では`../asset-transform-mcp`）の`release.yml`は、ビルド → GitHub Release → npm公開 → MCPレジストリ登録の4段。本changeは前半2段だけを、GoReleaserに置き換えて採る

## Goals / Non-Goals

**Goals:**

- タグのpushから公開まで、人の操作が1つも無いこと
- 利用者の端末で、コマンド・フック・MCPサーバーが同じ1本の常設バイナリであること
- リポジトリとCIに長期の認証トークンを1つも保存しないこと（使うのはGitHub Actionsが実行ごとに発行する`GITHUB_TOKEN`とOIDCだけ）

**Non-Goals:**

- `npx`やコンテナからの都度起動を導入経路にすること
- パッケージマネージャ（Homebrew、apt、winget等）への対応
- 自動更新。インストールスクリプトの再実行が更新手段である

## Decisions

### 1. npmラッパーを採らない

atx-mcpは`npx atx-mcp`を主経路にしているが、port-keeperでは採らない。

- READMEが案内するClaude Codeのフック（SessionStart等）と人の操作は、PATH上の`port-keeper`コマンドを呼ぶ。`npx`で手に入るのはMCPサーバーの起動だけで、コマンドは手に入らない
- `npx`のキャッシュに残った古いMCPサーバーと、別経路で入れた新しいコマンドが同じ台帳を触る、という版のずれを経路の側から作ってしまう
- メンテナの手作業が多い。atx-mcpの`RELEASING.md`の記録では、6パッケージの初回手動公開（2段階認証）、パッケージごとのTrusted Publisher登録、リリースごとのEnvironment承認、スパム誤検知の解除依頼、最長約30時間の反映待ちがあった

検討した代替: npmを副経路としてだけ残す案。手作業の量は主経路にする場合と変わらないので採らない。

### 2. ビルドとアーカイブはGoReleaser、公開の段取りはワークフロー側で持つ

`release.yml`は1ジョブで次の順に進む。

1. タグ名を環境変数経由で受け取り、`^v[0-9]+\.[0-9]+\.[0-9]+$`で検証する（タグ名にはシェルのメタ文字を含められるので、式を`run:`に直接埋め込まない。atx-mcpの方式）
2. 公開済み（下書きでない）のGitHub Releaseがすでにあれば、その旨を出して成功で終わる
3. タグをcheckoutし（`persist-credentials: false`、ビルドキャッシュは使わない）、`go test ./...`を通す
4. GoReleaserを実行する。`release.draft: true`と`release.replace_existing_draft: true`で、下書きのReleaseに6アーカイブと`checksums.txt`を載せる。アーカイブには`LICENSE`と`README.md`を同梱する
5. `dist/`のアーカイブと`checksums.txt`に`actions/attest-build-provenance`で来歴の証明を付ける
6. `gh release edit <tag> --draft=false`で公開する

権限は既定を`permissions: {}`にし、このジョブにだけ`contents: write`、`id-token: write`、`attestations: write`を付ける。GoReleaserのアクションと本体の版は固定する。

下書きで作って最後に公開するのは、リポジトリでImmutable releases（公開後のReleaseの成果物を変更できなくする設定）を有効にするためである。atx-mcpでは公開後に成果物を差し替えられないことが再実行時に問題になったので、手順2の早期終了を最初から入れる。

検討した代替: atx-mcpと同じくOS別のランナーで個別にビルドする案。Rustのネイティブ依存のための構成で、cgo不要のGoには要らない。GoReleaserに公開まで任せる案（`draft: false`）は、証明を付ける前に公開されてしまうので採らない。

チェックサムの一覧のファイル名はGoReleaserの既定の`checksums.txt`のままにする（atx-mcpは`SHA256SUMS`）。インストールスクリプトと`scripts/glama.sh`はこの名前を前提にする。

### 3. インストールスクリプト

`scripts/install.sh`（POSIX sh）と`scripts/install.ps1`。atx-mcpの同名スクリプトの構造を引き継ぐ。

- 入力は環境変数: `PORT_KEEPER_VERSION`（既定は最新のRelease）、`PORT_KEEPER_INSTALL_DIR`（既定`~/.local/bin`、Windowsは`%LOCALAPPDATA%\Programs\port-keeper`）
- 最新版の解決はGitHubのAPI（`releases/latest`の`tag_name`）で行う。当初はリダイレクトを読む案だったが、`wget`しか無い環境で同じ書き方ができないので、atx-mcpと同じAPI方式にした。認証なしのAPIは1時間に60回までなので、当たったら`PORT_KEEPER_VERSION`で版を指定する、とスクリプトのエラー文に書いた
- 取得はHTTPSに限定する（`curl --proto '=https' --tlsv1.2`）。`checksums.txt`と照合し、`sha256sum`も`shasum`も無い環境では失敗する（照合を飛ばす分岐を作らない）
- 一時ディレクトリに展開し、同じファイルシステム上での`mv`で置き換える（失敗時に既存のバイナリを壊さない）
- 台帳（`~/.local/state/port-keeper/`）と設定（`~/.config/port-keeper/`）には触れない
- READMEの1行コマンドは`main`ブランチのスクリプトを指す。スクリプトは版に依存しない作りにする

CIでは`shellcheck`と、偽のReleaseを立てたテスト（チェックサム不一致で何も置かないこと）を回す。実際のReleaseに対する通しの確認は`v0.1.0`の公開後に行う。

### 4. 台帳のスキーマの版

SQLiteの`PRAGMA user_version`を使う（台帳ファイルのヘッダにある整数で、表を足さずに済む）。

- 現在のスキーマを版1とする。`user_version`が0（未設定）の既存の台帳は版1として扱い、開いたときに1を書く
- 開く順序を「`user_version`を読む → 自分の版より大きければ、スキーマを流さずにエラー → 小さければ移行 → 同じなら何もしない」に変える。現在は無条件にスキーマを流しているので、この順序の変更が本体である
- エラーは型を分け、CLIは案内文と0以外の終了コード、MCPはツールのエラー結果にする。文面にポート番号を入れない（プロジェクトの規則）
- 複数プロセスが同時に新規の台帳を開く場合の再試行（既存）は保つ。版の書き込みはスキーマの作成と同じトランザクションに入れる

検討した代替: 版を入れる表を足す案。表の有無の判定が増えるだけで利点が無い。

### 5. MCP公式レジストリには`v0.1.0`では登録しない

レジストリが受け付ける形式はnpm／pypi／nuget／cargo／oci（コンテナイメージ）／mcpb（GitHub Releaseに置くまとめファイル）の6つである（レジストリ公式の文書で確認、2026-09-18）。npmは決定1で外れる。残る候補のうち検討したのはociとmcpbである。

- **oci**: Goのバイナリをコンテナに入れるのは容易だが、port-keeperの機能がコンテナの中では成立しない。(1) ポートが使われているかの確認は、自分のネットワーク上での`bind`と`lsof`で行う（`internal/probe/probe.go`）。コンテナの中から見えるのはコンテナのネットワークとプロセスで、ホストのものではない。macOSとWindowsではDockerがLinuxの仮想マシンの中で動くので、ホストのネットワークを共有する指定をしても届かない。(2) プロジェクトとスロットは、クライアントの作業ディレクトリから解決する。コンテナにはそのディレクトリが無い。(3) 台帳は利用者のホームにあり、マウントしない限り起動のたびに消える。レジストリに載せると、クライアントは`docker run`での起動を自動設定するので、「載っているが正しく動かない」経路を配ることになる
- **mcpb**: 採らない。調査（2026-09-18）で分かったこと: (1) `.mcpb`を導入できるクライアントはClaude Desktop（macOSとWindows）だけで、Claude Code、VS Code、Cursorには導入経路が無い。VS Codeは、mcpbしか持たないサーバーを一覧から落とす。(2) Claude Desktopには「開いているプロジェクトの作業ディレクトリ」が無く、起動されるサーバーの作業ディレクトリも文書化されていない。port-keeperはクライアントの作業ディレクトリからプロジェクトとスロットを解決するので、コンテナと同じ理由で成立しない。(3) コンパイル済みバイナリを同梱する種別（`server.type: "binary"`）は、macOSのClaude Desktopでは展開時に実行権限が落ちて起動しない不具合が未修理である（`modelcontextprotocol/mcpb`のissue #294）。(4) `.mcpb`の中のバイナリはPATHに入らないので、npmラッパーと同じく、版の違う2本のバイナリが1つの台帳を共有する。なお、当初の懸念だったCPU（amd64／arm64）の区別は、`server.json`の`packages`にOSとCPUごとの`.mcpb`を複数並べる方法で解決できる（Goの実例がレジストリに複数ある）ので、見送りの理由ではない（調査は別エージェントに委譲し、issue #294と下記のPRが開いていることは`gh`で再確認した。Claude Desktopでの実機確認はしていない）

したがって`v0.1.0`では登録しない。`server.json`からnpmの記述を除く。性質が合う形式は、レジストリに提案されている`go`形式（`go install`できるGoモジュールを登録する。バイナリはPATH上に常設される）で、2026-09-18時点では未マージである（`modelcontextprotocol/registry`のissue #1307とPR #1321）。`docs/ROADMAP.md`のM3（需要駆動）に、再検討の条件「PR #1321のマージ」を書く。発見の経路はGlamaとawesome-mcp-serversで確保する。

Glamaのチェックもコンテナでサーバーを起動するが、確かめるのは`initialize`と`tools/list`だけで、ポートの確認やプロジェクトの解決は走らない。そのためGlamaへの登録は上の問題に当たらない。

### 6. Glamaのローカル検証

atx-mcpの`scripts/glama.sh`を移植する。`check <version>`は、Glamaが生成するのと同じ構成のイメージをlinux/amd64で作り、GitHub Releaseのバイナリを`checksums.txt`で照合して置き、`mcp-proxy`越しに`initialize`と`tools/list`を送る。`form build-steps|cmd|placeholder`はGlamaの管理画面に貼る値を出す。CMDは`["port-keeper","mcp"]`で、必須の環境変数は無い。

由来: atx-mcpのv0.5.0は、ツールの`outputSchema`の最上位に`type: "object"`が無く、`mcp-proxy`（公式TypeScript SDK）が`tools/list`全体を拒否してGlamaのチェックに落ちた。サーバー側のテストでは検出できなかった。port-keeperのGo SDKが出すスキーマが同じ検証を通るかは未確認なので、公開前（タスク1）にも、ソースからビルドしたバイナリで同じ検証を行う。

## Risks / Trade-offs

- [初回のタグで`release.yml`が失敗する] → 下書きの段階で止まるので公開物は出ない。直して同じタグで再実行できる。事前に、フォークせずに済む範囲（`goreleaser release --snapshot --clean`のローカル実行と、`goreleaser check`のCI実行）で確かめる
- [`curl | sh`形式への不信] → スクリプトは短く保ち、READMEに手動ダウンロードと`gh attestation verify`の手順を併記する
- [インストールスクリプトが`main`から配られるので、`main`への変更が即座に利用者に届く] → `main`は保護済みでCI必須。スクリプトの変更は`shellcheck`とテストを通す
- [Glamaの画面構成が変わる] → `RELEASING.md`に「画面を正として進め、終わったら手順を直す」と書く（atx-mcpと同じ運用）
- [スキーマの版の導入で、開く順序を変える際に同時起動の再試行を壊す] → `internal/cli/cli_test.go`の複数プロセス同時実行のテストを`-race`で通す
- [MCP公式レジストリに載らないことで発見されにくい] → Glamaとawesome-mcp-serversに掲載し、GitHubのトピックは設定済み。レジストリに提案中の`go`形式（PR #1321）がマージされたら再検討する、とROADMAPに書く

## Migration Plan

1. 実機確認とスキーマの版、`release.yml`、インストールスクリプト、文書を`main`に入れる（それぞれ別のcommit）
2. `v0.1.0`をタグ付けしてpushする。失敗したら、下書きのReleaseを確認して同じタグで再実行する。タグの打ち直しはしない（タグは保護されている）
3. 公開物を確認する（アーカイブ6個、`checksums.txt`、`gh attestation verify`、インストールスクリプトでの導入、`doctor`が緑）
4. `scripts/glama.sh check 0.1.0`のあとGlamaに登録し、チェック通過後にawesome-mcp-serversへPRを出す
5. `initial-implementation`をアーカイブし、続けて本changeをアーカイブする

ロールバック: 公開したReleaseは消さない（Immutable releases）。不具合は`v0.1.1`で直す。
