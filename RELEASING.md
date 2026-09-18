# RELEASING

メンテナ（と、メンテナの指示で作業するコーディングエージェント）向けの手順書。port-keeper-mcpのリリースは、`v<major>.<minor>.<patch>`形式のgitタグをpushするだけで、GitHub Actionsの`.github/workflows/release.yml`が最後まで行う。人の承認の段も、保存した認証トークンも無い。

状態（2026-09-18）: 仕組みは実装済みで、最初のリリース`v0.1.0`はまだ出していない。したがって、この文書の手順は`release.yml`を実際のタグで動かした実績がまだ無い。初回に食い違いが見つかったら、この文書を直すこと。

各節はその節だけで読めるように書いてある。

## 1. リリースの手順

前提: 出したい変更がすべて`main`に入っていて、`main`のCIの3つのジョブが成功していること。`test (ubuntu-latest)`と`test (macos-latest)`はテストとlintで、`main`の保護で必須になっている。`release-config`はリリースの入力の検査（`shellcheck scripts/*.sh`と`goreleaser check`）で、必須には指定していないので、タグを打つ前に自分の目で確かめる。

```sh
git switch main && git pull
git tag --sort=-v:refname | head -3           # これまでのタグ。次の版はここから決める（何も出なければ初回のv0.1.0）
gh run list --branch main --limit 3          # 直近のCIが成功していることを確かめる
git tag v0.1.0                               # 版は実際の値に読み替える
git push origin v0.1.0
gh run watch "$(gh run list --workflow release.yml --limit 1 --json databaseId --jq '.[0].databaseId')"
```

- バージョンを書き込むファイルは無い。バイナリの版（`port-keeper --version`と、MCPの`initialize`応答の`serverInfo.version`）は、GoReleaserがタグから埋め込む。`server.json`の`version`はMCP公式レジストリに登録するときにだけ意味を持ち、現在は登録していないので触らない
- **タグのpushは、メンテナの明示の指示があったときだけ行う。** タグのpushは公開と同じである。「v0.1.1をリリースして」「タグを打ってpushして」は指示にあたる。「マージして」「commitして」「リリースの準備をして」はあたらない。迷ったら、打とうとしているタグ名を示して確認を取る
- **タグは打ち直せない。** `v*`タグは、リポジトリのルールセット「protect release tags」で更新と削除を禁じてある。間違えたら、次のパッチ版を出す
  - 由来: 公開後に中身の変わるタグは、利用者がチェックサムや来歴の証明で確かめた内容を後から裏切る。atx-mcp（同じ作者の別のMCPサーバー。このリリースの仕組みの手本）と同じ設定にした

## 2. `release.yml`がすること

1ジョブで、次の順に進む。

1. タグ名を検証する（`^v[0-9]+\.[0-9]+\.[0-9]+$`。合わなければ何も作らずに失敗）
2. そのタグのGitHub Releaseがすでに公開済みなら、何もせずに成功で終わる
3. タグのソースをcheckoutし、`go test ./...`を通す
4. GoReleaser（設定は`.goreleaser.yaml`）で6アーカイブ（darwin／linux／windows × amd64／arm64）と`checksums.txt`を作り、**下書きの**GitHub Releaseに載せる
5. linux/amd64のバイナリを実行し、`--version`がタグの版と一致することを確かめる
6. アーカイブと`checksums.txt`にビルド来歴の証明（build provenance attestation。どのワークフローがどのcommitから作ったかの署名つき記録）を付ける
7. 下書きを公開する

- 下書きで作って最後に公開するのは、リポジトリでImmutable releases（公開後のReleaseの成果物を変更できなくするGitHubの設定）を有効にしてあるため。公開した時点で成果物は固定される
  - 由来: atx-mcpで、公開後のReleaseに対する再実行が成果物を上書きしようとして失敗した。そのため手順2の「公開済みなら何もしない」を最初から入れた
- ビルドキャッシュは使わない（`setup-go`の`cache: false`）。ブランチのビルドが書いたキャッシュが、配布するバイナリに入りうるため
- GoReleaser本体の版（`release.yml`と`ci.yml`の`version: v2.x.y`）はDependabotの対象外で、手で上げる。上げるときは2つのファイルを同時に直し、ローカルで`go run github.com/goreleaser/goreleaser/v2@<版> check`を通す。アクション（`goreleaser/goreleaser-action`など）はcommit SHAで固定してあり、Dependabotが更新のPRを出す

## 3. 公開物の確認

```sh
gh release view v0.1.0 --json isDraft,isImmutable,assets --jq '{isDraft, isImmutable, assets: [.assets[].name]}'
# 期待: isDraft=false、isImmutable=true、アーカイブ6個とchecksums.txt

tmp=$(mktemp -d) && cd "$tmp"
gh release download v0.1.0 --repo gridhra/port-keeper-mcp --pattern '*darwin_arm64.tar.gz' --pattern checksums.txt
shasum -a 256 -c --ignore-missing checksums.txt
gh attestation verify port-keeper_*_darwin_arm64.tar.gz --repo gridhra/port-keeper-mcp   # 終了コード0
tar -xzf port-keeper_*_darwin_arm64.tar.gz port-keeper && ./port-keeper --version          # 版がタグと一致
```

続けて、利用者と同じ経路で入れ直して確かめる。

```sh
curl -fsSL https://raw.githubusercontent.com/gridhra/port-keeper-mcp/main/scripts/install.sh | sh
port-keeper --version && port-keeper doctor
```

- 期待: 版がタグと一致し、`doctor`が緑。その端末でport-keeperを使っていたなら、`port-keeper status`（プロジェクトの中で実行）で以前のリースがそのまま見えること。初めて入れる端末なら、適当なディレクトリで`port-keeper init --name demo && port-keeper env`が通ることを確かめる
- 同じ端末に`go install`で入れた`port-keeper`が残っていると、`PATH`の順で古いほうが呼ばれることがある。`which -a port-keeper`で確かめ、古いほうを消す

## 4. 失敗したときのやり直し

- **公開前に失敗した（手順1〜6のどこか）**: 原因を直す。ワークフローの側の問題なら、修正を`main`に入れても、タグのソースには入らないことに注意する（`release.yml`自体はタグの時点のものが使われる）。その場合は次のパッチ版のタグを打つ。一時的な失敗（ネットワークなど）なら、Actionsの画面の「Re-run failed jobs」か、`gh workflow run release.yml -f tag=v0.1.0`で同じタグを再実行する。下書きのReleaseが残っていても、GoReleaserが作り直す（`replace_existing_draft: true`）
- **公開後に不具合が見つかった**: Releaseは消さない（消しても、すでに入れた人の手元は変わらず、タグは再利用できない）。直して次のパッチ版を出す
- **公開済みのタグで再実行してしまった**: 手順2で何もせずに終わるので、害は無い

## 5. リポジトリの設定（確認日: 2026-09-18）

`gh api`で読んだ結果。変えたときは、この節の日付と内容を直す。

| 設定 | 値 | 確認のしかた |
|---|---|---|
| `main`の保護 | 必須のチェック: `test (ubuntu-latest)`、`test (macos-latest)`。`release-config`（リリースの入力の検査）は必須にしていない | `gh api repos/gridhra/port-keeper-mcp/branches/main/protection` |
| タグのルールセット「protect release tags」 | `refs/tags/v*`の更新・削除・強制pushを禁止（作成は可）。例外の指定は無し | `gh api repos/gridhra/port-keeper-mcp/rulesets` |
| Immutable releases | 有効（2026-09-18に有効化。それまでは無効だった） | `gh api repos/gridhra/port-keeper-mcp/immutable-releases` |
| Secrets | 無し。`release.yml`が使うのは、実行ごとに発行される`GITHUB_TOKEN`と、OIDC（GitHub Actionsがその実行の身元を証明する短命のトークンを発行する仕組み。ビルド来歴の証明の署名に使う）だけ | `gh secret list` |

## 6. 配布物の名前を変えるとき

アーカイブの名前（`port-keeper_<版>_<os>_<arch>.tar.gz`／`.zip`。バイナリはアーカイブの直下）と`checksums.txt`の名前には、次の5か所が依存している。変えるなら同じcommitで全部直す。

- `.goreleaser.yaml`（名前の出どころ）
- `scripts/install.sh`と`scripts/install.ps1`
- `scripts/glama.sh`（Glamaのビルド手順）
- `README.md`／`README.ja.md`／`README.zh-CN.md`の手動インストールの手順
- `release.yml`の「Check the built version」（`dist/port-keeper_linux_amd64_v1/port-keeper`というGoReleaserの出力パスを使っている）

インストールスクリプトを変えたら、`sh scripts/install_test.sh`（偽のReleaseに対するテスト。ネットワーク不要）と`shellcheck scripts/*.sh`を通す。どちらもCIで走る。スクリプトは`main`から直接配られるので、`main`に入った時点で利用者に届く。

## 7. やらないと決めたこと

理由の全文は、`docs/DESIGN.md`の§9.2（初回配布の準備で確定した差分。4項目すべての理由がある）と、READMEの「Non-goals」（コンテナイメージと`npx`）にある。要望が来たら、まずそこを読んでもらう。

- **npmラッパー（`npx port-keeper-mcp`）**: 作らない。フックと人の操作がPATH上の`port-keeper`コマンドを必要とするので、都度起動のランチャーでは足りない。版の違うバイナリが1つの台帳を共有する原因にもなる
  - 由来: atx-mcpはnpmで配っており、6パッケージの初回手動公開（2段階認証）、パッケージごとの公開設定、リリースごとの承認、npmのスパム誤検知の解除依頼、最長約30時間の反映待ちが発生した
- **コンテナイメージ**: 配らない。port-keeperはホストのネットワーク、ホストのプロセス一覧、クライアントの作業ディレクトリ、ホームの下の台帳を直接見るが、コンテナはその4つを隔離する
- **MCP公式レジストリ**: 未登録。受け付ける形式のうち、npmとoci（コンテナ）は上の理由で使えない。mcpb形式（GitHub Releaseに置くまとめファイル）は、導入できるクライアントがClaude Desktopだけで、そこにはport-keeperが必要とする「プロジェクトの作業ディレクトリ」が無く、macOSではバイナリ同梱型が起動しない不具合も未修理なので、採らない（2026-09-18の調査。詳細は`docs/DESIGN.md`の§9.2）。レジストリに提案中の`go`形式（`modelcontextprotocol/registry`のPR #1321。`go install`できるモジュールの登録）がマージされたら再検討する
- **Homebrew tap**: 未着手。tap用の別リポジトリと、そこへ書き込む長期トークンの管理が要る（同じくROADMAPのM3）

## 8. Glama

Glama（https://glama.ai 。MCPサーバーの登録・評価サイト）は、awesome-mcp-servers（MCPサーバーの一覧。GitHubの`punkpeye/awesome-mcp-servers`）への掲載条件になっている。Glamaは、**管理画面のフォームから生成したDockerfileでサーバーを起動し、`mcp-proxy`（stdioのMCPサーバーをHTTPで中継する道具。公式TypeScript SDK製）越しに`tools/list`を取れるか**を調べる。フォームのビルド手順はバイナリの版を固定するので、**port-keeperをリリースするたびに、Glama側もフォームを更新してリリースし直す**。

Glamaが使うコンテナは、この動作確認のためだけのものである。送られるのは`initialize`と`tools/list`だけで、ポートの確認やプロジェクトの解決は走らない。「コンテナイメージは配らない」という方針とは矛盾しない。

状態（2026-09-18）: 登録済み。Glama上のリリース`0.1.0`を公開し、READMEにスコアのバッジを載せた。この節の手順は、すべてport-keeperで一度通した。

### 8.0 初回だけ: サーバーを審査に出す（2026-09-18に実施）

Glamaは、登録されていないリポジトリのDockerfileの設定画面を開かせない。先に https://glama.ai/mcp/servers の「Add Server」から審査に出し、承認のメールを待つ。

1. GitHubアカウント`gridhra`でログインする（画面右上が「Sign Up」なら未ログイン。ログインは人が行う）
2. https://glama.ai/mcp/servers の右上「Add Server」を押す。「Runs from source」のタブのまま、3つの欄を入れて「Submit for Review」を押す

| 欄 | 入れた値 |
|---|---|
| Name | `port-keeper-mcp` |
| Description | `Local ledger for development ports: leases a block of ports per project slot (one per working copy), renders env files, and resolves service names to URLs. No daemon, no listener.` |
| GitHub Repository URL | `https://github.com/gridhra/port-keeper-mcp` |

3. 「Your server has been submitted for review」と出れば送信済み。送信の直後は、サーバーのページ（`/mcp/servers/gridhra/port-keeper-mcp`）はまだ「not found」のままである
4. 承認されると「has been approved and listed on Glama」というメールが届く（初回は数十分だった）。登録したGitHubアカウントがリポジトリの所有者なので、所有の申告（claim）は別に要らず、承認と同時にAdminタブが使える。届いたら8.1から進める

作業はClaude CodeがChrome（Claude in Chrome拡張）で操作し、ログインなどの認証だけを人に頼む前提で書く。

### 8.1 先にローカルで検証する

```sh
sh scripts/glama.sh check 0.1.0
```

Glamaが生成するのと同じ構成（`debian:trixie-slim`とNodeと`mcp-proxy`）のイメージをlinux/amd64で作り、GitHub Releaseのバイナリを`checksums.txt`で照合して置き、`mcp-proxy`越しに`initialize`と`tools/list`を送る。`serverInfo`の版が一致し、ツールの一覧が取れれば`OK`を出す。検証用のイメージとコンテナは終了時に消える。Dockerのデーモンとnodeが要る。

リリース前に確かめたいときは、ローカルでビルドしたバイナリを使う。

```sh
go run github.com/goreleaser/goreleaser/v2@v2.18.2 release --snapshot --clean
sh scripts/glama.sh check-local dist/port-keeper_linux_amd64_v1/port-keeper
```

- 由来: atx-mcpのv0.5.0は、ツールの`outputSchema`の最上位に`type: "object"`が無く、`mcp-proxy`が`tools/list`全体を拒否してGlamaのチェックに落ちた。サーバー側のテストでは検出できなかった。port-keeperでは2026-09-18に`check 0.1.0`で6ツールの一覧が取れることを確かめてある
- Glamaのフォームの既定値は変わる。2026-09-18の初回登録では、`mcp-proxy`の版が手元のスクリプト（6.4.3）と画面のプレビュー（6.7.16）で違っていた。8.2の手順1のとおり、スクリプトの変数を画面に合わせてから検証をやり直した
- 失敗したら、Glamaには触らずに原因を直す。Glamaに出しても同じ理由で落ちる

### 8.2 フォームを更新してビルドする

画面: Glamaのport-keeper-mcpのページ → Admin → Dockerfile。

| 欄 | 入れる値 |
|---|---|
| Base image | `debian:trixie-slim`（既定のまま） |
| Node.js version／Python version | 既定のまま |
| Build steps | `sh scripts/glama.sh form build-steps 0.1.0`の出力 |
| CMD arguments | `sh scripts/glama.sh form cmd`の出力（`["port-keeper", "mcp"]`。Glamaが前に`mcp-proxy --`を付ける） |
| Environment variables JSON schema | 既定のまま（port-keeperに必須の環境変数は無い） |
| Placeholder parameters | `sh scripts/glama.sh form placeholder`の出力（`{}`） |
| Pinned commit SHA | **空欄** |

1. 右側のDockerfileのプレビューを読み、`scripts/glama.sh`の先頭の変数（ベースイメージ、Nodeの版、`mcp-proxy`の版）と食い違っていないかを見る。違ったらスクリプトの変数を合わせ、8.1をやり直す
2. **「Build」を押す**（「Build & Release」ではない）。テスト結果のページに移る

入力のこつ（Claude CodeがChromeで操作するとき）。いずれもatx-mcpでの経験による。

- 入力欄はCodeMirror（コードエディタの部品）で、普通のtextareaではない。値は`sh scripts/glama.sh form ... | pbcopy`でクリップボードに入れ、欄をクリック → cmd+a → cmd+vで貼る
  - 由来: JavaScriptでDOMに値を入れようとしたら、既存の値の後ろに追記されたうえ、ページが応答しなくなった。キー入力で入れると、エディタが括弧や引用符を自動で補って崩れる
- Pinned commit SHAは空欄にする。ビルド手順はリポジトリの中身を使わない（バイナリをReleaseから取る）
  - 由来: Glamaがまだ同期していないcommitを入れたら、「Commit not found」で弾かれた

### 8.3 結果を確認して、Glamaのリリースを作る

1. テスト結果のページは自動では更新されない。**再読み込みして**Statusを見る（`pending` → `testing` → `success`。`pending`はGlama側の順番待ちで、数分以上続くことがある）
2. 「Instance logs」に、`"result":{"tools":[`を含む行があることを確かめる
3. ページの下の「Create Release」を押す。Version欄の既定値はGlamaが決めた値で、port-keeperの版とは無関係である（atx-mcpでは、初回は`0.1.0`、2回目以降は前回のGlama上の版の次のパッチ版が入っていた）。**必ずport-keeperの版と一致しているかを見て、違えば直す**。Changelogは英語で1行。「Create & Publish Release」を押す
4. Admin → Releasesに、その版が`latest`として出れば完了

### 8.4 注意

- **Auto-Release**（Admin → Releasesの切り替え。GitHub ReleaseのたびにGlamaが自動でビルドとリリースを行う機能）は**オフにする**。登録直後の既定はオンだった（2026-09-18）ので、初回は8.3の前にオフにする
  - 由来: ビルド手順がバイナリの版を固定しているので、自動で作られる版は、フォームに残っている古い版のバイナリになり、それが`latest`として出てしまう（atx-mcpで確認）
- ログインは人が行う。Claude Codeは、ログイン画面に来たら止まって依頼する
- Glamaの画面の構成やフォームの既定値は変わりうる。この節の記述と画面が違ったら、画面を正として進め、終わったらこの節を直す

### 8.5 awesome-mcp-serversへの掲載

条件は、Glamaでの登録（所有者として）、Glamaのチェックの通過、READMEのGlamaのバッジの3つ。バッジは、Admin → GitHub Badgeにある「Score Badge」のマークダウンを、3言語のREADMEの言語切り替えの行の直下に置いた（2026-09-18）（点数の下限は無い。2026-09時点、atx-mcpで確認）。書式は、その時点の`punkpeye/awesome-mcp-servers`の`CONTRIBUTING.md`と、atx-mcpの掲載行（同リポジトリのREADMEで`atx-mcp`を検索）を手本にする。分類は「Developer Tools」を第一候補とし、説明のたたき台は「Local ledger for development ports: leases a block per project slot, renders env files, resolves service names to URLs. No daemon, no listener.」。PRは外部への送信なので、Claude Codeは文面（分類、1行の説明、バッジ）をメンテナに示して確認を取ってから出す。出したPRのURLは、ここに記録する。

- PR: https://github.com/punkpeye/awesome-mcp-servers/pull/14635 （2026-09-18提出。英語版に加えて`README-ja.md`と`README-zh.md`にも1行ずつ足した。翻訳版は本家が同期するのではなく、投稿者が任意で足す運用で、そうしたPRがマージされた実績がある）

## 9. 文書

利用者向けの文書を変えるときは、`README.md`（原本）、`README.ja.md`、`README.zh-CN.md`を同じcommitで直す。見出しの数とコードブロックの数を揃える。
