# Tasks

## 1. 公開前の実機確認

- [x] 1.1 この端末に`go install ./cmd/port-keeper`で入れ、`claude mcp add --scope user port-keeper -- port-keeper mcp`とREADMEのフック設定（SessionStart／CwdChanged）を行う。端末の`~/.claude/settings.json`とMCP登録を変えるので、着手前にユーザーへ変更内容を示して確認を取る。確認: 新しいClaude Codeセッションで`/mcp`に`port-keeper`が接続済みと出る
- [x] 1.2 架空の`shop`プロジェクト（一時ディレクトリ）で、既定で有効な6ツールと、設定で有効化する2ツールを実セッションから1回ずつ呼ぶ。確認: 全ツールが成功し、番号を返してよい3ツール（`resolve_url`／`resolve_port`／`render_env`）以外の結果にポート番号が無い。不具合は1件ずつ別commitで直す
- [x] 1.3 フックの動作を確かめる。確認: 作業コピー（cloneまたはgit worktree）を切り替えたセッションで、フックの出力にそのスロットの文脈が入る
- [x] 1.4 ソースからビルドしたlinux/amd64バイナリを、Glamaと同じ構成（`debian:trixie-slim`とNodeと`mcp-proxy`）のコンテナに入れ、`port-keeper.toml`の無いディレクトリで`initialize`と`tools/list`を送る。確認: ツール一覧が返り、スキーマ検証で拒否されない（specの「公式TypeScript SDKクライアントとの互換」）

## 2. 台帳のスキーマの版

- [x] 2.1 `internal/ledger`で、開くときに`PRAGMA user_version`を読み、自分の版より新しければスキーマを流さずに専用のエラーを返し、0なら版1を書くようにする。確認: specの`port-ledger`の4シナリオに対応するテスト（新しい台帳の拒否で台帳のバイト列が変わらないことを含む）が通る
- [x] 2.2 CLIとMCPでそのエラーを案内文にする。確認: CLIは0以外で終了して更新を案内し、MCPのツールはエラー結果を返し、どちらの文面にもポート番号が無いことをテストで検証する
- [x] 2.2a 実装中に見つけた既存の不具合を直す: `render_env`の失敗がプロトコルエラーになる（失敗時の出力の`env`がnull）。確認: プロジェクトの外で全ツールを呼び、どれもツールのエラーとして返ることのテスト（`TestFailuresAreToolErrors`）が通る。要件は`specs/mcp-tools/spec.md`に追加
- [x] 2.3 `go test -race ./...`（複数プロセス同時実行のテストを含む）、`go vet ./...`、`gofmt -l .`、`GOOS=windows go build ./...`が通ることを確認する
- [x] 2.4 `docs/DESIGN.md`の§9（実装中に変わった点）に、スキーマの版を入れた理由と開く順序を追記する。確認: その節だけを読んで理由が分かる

## 3. リリースの自動化

- [x] 3.1 `.goreleaser.yaml`を仕上げる（`release.replace_existing_draft: true`、アーカイブへの`LICENSE`と`README.md`の同梱、先頭の「未実行」コメントの更新）。確認: `goreleaser check`が通り、`goreleaser release --snapshot --clean`で6アーカイブと`checksums.txt`ができ、darwin/arm64のバイナリの`--version`がスナップショットの版を返す
- [x] 3.2 `.github/workflows/release.yml`を作る（タグ名の検証、公開済みReleaseでの早期終了、テスト、GoReleaser、来歴の証明、下書きの公開。アクションはcommit SHAで固定、既定の権限は無し）。確認: `actionlint`が通り、design.mdの決定2の6手順と1対1で対応している
- [x] 3.3 `ci.yml`に`goreleaser check`を足す。確認: PRのCIで実行されて成功する
- [x] 3.4 GitHubのリポジトリ設定を確かめ、足りなければ設定する（Immutable releasesの有効化、`v*`タグの保護ルールセット、`main`の保護）。確認: `gh api`での読み取り結果を`RELEASING.md`の該当節に日付つきで記録する

## 4. インストールスクリプト

- [x] 4.1 `scripts/install.sh`を作る（OS／CPUの検出、最新版の解決、HTTPS限定の取得、`checksums.txt`との照合必須、一時ディレクトリからの置き換え、PATHの案内）。確認: `shellcheck`が通る
- [x] 4.2 `scripts/install.ps1`を作る（同じ振る舞いのWindows版）。確認: `pwsh`での構文検査が通る。Windowsの実機確認は範囲外であることをスクリプトの先頭とREADMEに書く
- [x] 4.3 `install.sh`のテストを足す（ローカルに立てた偽のReleaseに対して、正常系、チェックサム不一致で何も置かず既存のバイナリが残ること、対応していない環境の案内）。確認: `ci.yml`のubuntuとmacosの両方で通る

## 5. 文書

- [x] 5.1 `README.md`のQuickstartを「インストールスクリプト／手動ダウンロードと`gh attestation verify`／ソース」の3経路に書き換え、「Planned install channels」の段落を削る。`README.ja.md`と`README.zh-CN.md`を同じcommitで鏡写しにする。確認: 3ファイルの見出し数とコードブロック数が一致し、`README.ja.md`の日本語の文中に、半角英数字の前後の半角スペースが無い（`CLAUDE.local.md`が挙げる検査スクリプト`~/.claude/scripts/ja-spacing.py`は2026-09-18時点でこの端末に無いので、正規表現での検索で代える）
- [x] 5.1a `README.md`の「Non-goals」に「No container image」の項を足す（既存の項と同じ強い調子で、理由の4点と代わりの導入経路を書く）。「If you still want one of these」の節との整合も取り、`README.ja.md`と`README.zh-CN.md`を同じcommitで鏡写しにする。確認: 3ファイルの見出し数が一致し、specの「コンテナイメージを配布しない」の1つ目のシナリオを満たす
- [x] 5.2 `RELEASING.md`を新設する（リリース手順、公開物の確認方法、失敗したときのやり直し、Glamaの手順、各規則にその由来を一つ添える）。確認: この文書だけを渡された別セッションが`v0.1.1`を出せる内容になっているかを、コンテキストを持たないサブエージェントに読ませて不明点を挙げさせる
- [x] 5.3 `server.json`からnpmの記述を除く。`docs/ROADMAP.md`のM2を実態に合わせ（npmラッパーは採らない、と理由）、M3にHomebrew tapとMCP公式レジストリ（mcpb形式は調査の結果採らない。レジストリに提案中の`go`形式のマージを再検討の条件にする）を移す。`CLAUDE.md`の「現状」と、リリース手順への入口を更新する。確認: `rg -n "npx|npm" README*.md docs server.json CLAUDE.md`の結果が、採らない理由の説明だけになる
- [x] 5.4 `initial-implementation`のタスク4.3を「npmラッパーは採らない。Homebrew tapとMCPレジストリはROADMAPのM3へ移した」と書き換えて閉じ、アーカイブする。確認: `openspec list`に`initial-implementation`が出ず、`openspec/specs/`に6 capabilityの主specができる

## 6. v0.1.0の公開

- [x] 6.1 1〜5が`main`に入りCIが緑であることを確かめてから、ユーザーの明示の指示を受けて`v0.1.0`をタグ付けしてpushする。確認: `release.yml`が成功する
- [x] 6.2 公開物を確かめる。確認: `gh release view v0.1.0 --json isDraft,assets`が`isDraft:false`とアーカイブ6個＋`checksums.txt`を返し、`gh attestation verify`が成功し、取得したバイナリの`--version`が`0.1.0`を含む
- [x] 6.3 この端末の`go install`版を消し、READMEの1行コマンドで入れ直す。確認: `port-keeper --version`が`0.1.0`で、既存の台帳のリースが残り、`port-keeper doctor`が緑。ROADMAPのM2の完了条件「新しい端末で5分で導入」に照らして所要時間を記録する

## 7. 掲載

- [x] 7.1 `scripts/glama.sh`を作る（`check <version>`と`form build-steps|cmd|placeholder`）。確認: `sh scripts/glama.sh check 0.1.0`が`OK`を出す
- [x] 7.2 Glamaに登録する。Chromeの操作はClaude Codeが行い、ログインと所有の申告はユーザーが行う。自動リリースの設定はオフにする。確認: Glamaのチェックが`success`になり、Glama上のリリース`0.1.0`が`latest`と表示される
- [x] 7.3 READMEの3言語にGlamaのバッジを足す。確認: 3ファイルが同じcommitで更新されている
- [ ] 7.4 awesome-mcp-serversへの掲載PRを用意する（分類、1行の説明、バッジ）。外部への送信なので、文面をユーザーに示して確認を取ってから出す。確認: PRのURLを`RELEASING.md`に記録する
- [ ] 7.5 本changeをアーカイブする。確認: `openspec list`が空で、`openspec/specs/distribution/spec.md`ができ、`port-ledger`の主specにスキーマの版の要件が入っている
