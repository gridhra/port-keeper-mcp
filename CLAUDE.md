# port-keeper-mcp — コーディングエージェント向けの覚え書き

まず`README.md`（何をするものか、保証、非目標）を読み、次に`docs/DESIGN.md`（設計。§9に実装中に変わった点とその理由）を読む。

## コマンド

```sh
go test -race ./...            # 全テスト。commit前に必ず通す
go vet ./... && gofmt -l .     # lint。gofmtの出力は空でなければならない
GOOS=windows go build ./...    # Windowsでもコンパイルが通る状態を保つ（CIが検査する）
go run ./cmd/port-keeper       # ソースから実行
sh scripts/install_test.sh     # インストールスクリプトのテスト（ネットワーク不要）
sh scripts/readme_sync_check.sh # README 3言語の見出し・コードブロック・表の行の照合（CIでも走る）
sh scripts/agent_eval.sh --list # エージェントeval（本番は実際のモデルを動かすのでリリース前に手動。RELEASING.md）
openspec list                  # 進行中のOpenSpec change
```

Go 1.25以上（ツールチェーンは自動取得される）。テストは`App.Probe`と`App.Listeners`を差し替えるので、実際のポートにbindしたり`lsof`を走らせたりしない。この方針を崩さないこと。`internal/cli/cli_test.go`はテストバイナリ自身を再実行して、本物の複数プロセスの同時実行を検証している。

## 破りやすい規則

- **マニフェスト、git、ログ、そして`current_context`／`status`／`slot_new`／`list_all_projects`の出力に、ポート番号を含めてはならない。** 番号を出してよいのは`resolve_url`、`resolve_port`、`render_env`と`env`だけ（CLIの`status`と`status --json`は表に番号を出すが、MCPの`status`ツールは出さない）。
- **`doctor`のMCP設定検査は、設定ファイルの値を出力しない。** 報告するのはファイルパスと項目名と「何が見つかったか」だけ。`.env.local`の古さや追跡済み`.env`の警告も、番号ではなく変数名で言う。
- **待ち受けなし、デーモンなし、プロセスを殺さない、台帳に秘密情報を入れない。** READMEの非目標（Non-goals）。これらのどれかが必要になる変更はPRではなく設計の相談。
- **書き込みトランザクションの中での読み取りは`*ledger.Tx`経由**にする。`*ledger.Ledger`経由は接続が1本なのでデッドロックする。
- **スロットの解決順**は `--slot` → 作業コピーの紐づけ → `PORT_KEEPER_SLOT` → `slot_default`。自分のスロットを持たない作業コピーには既定スロットを渡さない（`Context.RequireBound`）。
- **クライアント固有のコードはアダプタに閉じ込める**（`internal/cli/hook_claude.go`）。エージェント向けの核の契約は`context --json`＋`env --format export`。
- MCPツールの省略可能な入力には`omitempty`を付ける（SDKが入力スキーマを検証する）。状態を変えるツールに`ReadOnlyHint: true`を付けない。
- 文書とメッセージでは「worktree」ではなく「作業コピー（working copy。cloneまたはgit worktree）」と書く。

## 進め方

- 変更はOpenSpecのchange（`/opsx:propose`）から始める。軽微な修正は例外。changeは完了させたPRの中でアーカイブする。
- commit: `<type>: <subject>`を英語で、要点を数行の箇条書きで、1commitに1つの関心事。`git add <ファイル>`を明示し、`-A`は使わない。`main`は保護されていてCI（`test (ubuntu-latest)`、`test (macos-latest)`）の成功が必須。直接pushは小さくテスト済みの変更に限る。
- 文書: `README.md`が原本。`README.ja.md`と`README.zh-CN.md`は節ごとの鏡写し（見出し数・コードブロック数・表の行の並びを揃える。`sh scripts/readme_sync_check.sh`が検査する）で、同じcommitで更新する。`docs/examples/`は英語のみで翻訳しない。`docs/DESIGN.md`、`docs/ROADMAP.md`、`openspec/`は日本語。issueテンプレートは英語と日本語の両方がある。進捗・予定・タスクの一覧は`CLAUDE.md`ではなく`docs/ROADMAP.md`に書く。
- READMEに「見れば分かる」「エージェントに聞けば分かる」類の注記を足さない。非目標の節の強い調子は保つ。
- 日本語の文中で、半角英数字の前後に半角スペースを入れない（`AIに開発させる`であって`AI に開発させる`ではない）。

## どこを読むか

進捗と予定はこのファイルに書かない（毎セッション読み込まれるので、消化したら消える情報を置かない）。何がどこまで済んでいて次に何をするかは`docs/ROADMAP.md`、実装中に決まったことと理由は`docs/DESIGN.md`の§9、リリース・やり直し・Glamaの手順は`RELEASING.md`、やらないと決めたことはREADMEのNon-goalsと`docs/DESIGN.md`の§9.2を読む。

配布まわりで破りやすい規則:

- **アーカイブ名（`port-keeper_<版>_<os>_<arch>`）と`checksums.txt`の名前を変えるときは、`.goreleaser.yaml`、`scripts/install.sh`、`scripts/install.ps1`、`scripts/glama.sh`、READMEの手動インストール手順を同時に直す。**
- **台帳のスキーマを変えるときは`ledger.SchemaVersion`を上げ、`migrate`に移行の段を足す。** 古いバイナリが新しい台帳を拒否できるのは、この版のおかげである。
- **MCPツールの出力の型にmapを足すときは、失敗時の出力でもnilにしない**（SDKはエラー結果でも出力スキーマを検証し、nilのmapは`null`になってプロトコルエラーになる。`render_env`で実際に起きた）。
