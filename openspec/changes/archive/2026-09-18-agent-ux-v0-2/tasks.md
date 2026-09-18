# Tasks

## 1. Goのコード

- [x] 1.1 `slot new --from-branch`（`gitx.Branch`、`app.SlotNameFromBranch`）。確認: `TestSlotNameFromBranch`（正規化の表）と`TestSlotNewFromBranch`（併用エラー、detached、リポジトリ外）が通る
- [x] 1.2 `status --json`。確認: `TestStatusJSON`（1行、項目、`--slot nope`で1）が通る
- [x] 1.3 driftの検出（`render.DotenvBlock`、`App.EnvDrift`、`context --json`の`env_stale`、フックの短い形、`doctor`の`[warn]`）。確認: `TestDotenvBlock`、`TestEnvDrift`（6段階の理由文に番号が無い）、`TestContextReportsEnvDrift`（CwdChangedの`systemMessage`に指示が入る）が通る
- [x] 1.4 `internal/doctor`（`MCPConfigs`、`TrackedDotenv`）と`cmdDoctor`への接続、`[fail]`で終了コード1。確認: `TestMCPConfigsWarnsOnPortsAndSecrets`（値が出ない）、`TestTrackedDotenv`（`.env.example`と未追跡は無視）、`TestDoctorChecksAndExitCode`が通る
- [x] 1.5 `completion zsh|bash|fish`と隠し`__complete`。確認: `TestCompletionScripts`、`TestCompleteCandidates`（プロジェクト外は無出力で0、番号なし）が通り、zshとbashの関数を試験用プロジェクトで動かして候補が出る（fishは未導入のため未確認）
- [x] 1.6 `go test -race ./...`、`go vet ./... && gofmt -l .`、`GOOS=windows go build ./...`。確認: 全部通る（2026-09-18）

## 2. スクリプトとCI

- [x] 2.1 `scripts/readme_sync_check.sh`。確認: 3ファイルで0終了。表の行を1言語だけ変えると差分を出して1終了（README編集中に実際に検出した）
- [x] 2.2 `scripts/agent_eval.sh`の`--list`／`--dry-run`／本番の経路。確認: `--list`と`--dry-run`がAPI無しで0終了。stream-jsonを模した`claude`のスタブでPASS／FAIL／setup failureの判定を確認し、本番（4.1）で実際の出力に対して通した
- [x] 2.3 `ci.yml`: `test`に同期検査と`--dry-run`、`release-config`に`--list`。確認: pushのあとCIが緑（未実施。pushはユーザーの指示待ち）
- [x] 2.4 shellcheck。確認: 初回のpushで`release-config`がSC2317（trap経由でだけ呼ぶ関数を到達不能と誤検知）で落ち、`# shellcheck disable=SC2317`を付けて通した

## 3. 例と文書

- [x] 3.1 `docs/examples/`の8ファイル（英語）。確認: 4〜5桁の数はdocker-compose.mdのコンテナ内部ポート`5432`だけ
- [x] 3.2 README 3言語（「Fits what you already run」のリンク、`context --json`の説明、Non-goalsのリンク、Security model、CLI表の5行、Development）。確認: `sh scripts/readme_sync_check.sh`が0終了、ja.mdに半角英数字の前後の半角スペースが無い
- [x] 3.3 `docs/DESIGN.md` §9.3、§9.1の未実装行、§5.5。`docs/ROADMAP.md`のM2.5とM3と規律5。`RELEASING.md`の状態と手順と§9。`SECURITY.md`の範囲。`CLAUDE.md`。確認: §9.3だけ読んで決定と理由が分かる

## 4. リリース前

- [x] 4.1 本番のエージェントeval（定額プランのログインで実行。`--bare`はAPIキー必須なので`--setting-sources local`に変えた）。確認: stream-jsonの項目名は想定どおり。初回は3つともFAILで、`slot_new`の要約文・サーバーの`Instructions`・`render_env`の説明・READMEの指示ブロックを直し（`docs/DESIGN.md` §9.3）、往復の数え方をport-keeperとのやりとりだけに絞り、`cat ./dev.sh`を起動と誤認する判定を直した。最終結果（2026-09-18、`sonnet`）:

  ```
  scenario calls/max tools                                    numbers/max   cost result
  s1             1/2 resolve_url=1                                    1/1 0.061201 PASS
  s2             5/5 Bash=3,current_context=1,render_env=1,slot_new=1         0/0 0.109825 PASS
  s3             2/4 Bash=2,current_context=1                         0/1 0.0620642 PASS
  ```
- [x] 4.2 `main`にcommitしてpushし、CIの3ジョブが緑であることを確かめた（2026-09-18、10commit）
- [x] 4.3 `v0.2.0`のタグをpushした（ユーザーの指示「release」による）。確認: `release.yml`成功、Releaseは公開済みで変更不可、6アーカイブ＋`checksums.txt`、チェックサムと来歴の証明が通り、インストールスクリプトで入れ直した`port-keeper --version`が`0.2.0`、`doctor`緑、既存のリースはそのまま
- [x] 4.4 Glamaの版を更新した（`RELEASING.md` §8。`glama.sh check 0.2.0`がOK、フォームのBuildが`success`で6ツール、Create Releaseで版を`0.2.0`に直して公開）。確認: Admin → Releasesで`0.2.0`が`Latest`、Auto-Releaseはオフ
- [x] 4.5 本changeをアーカイブし、主specに`doctor`と`shell-completion`を同期した。確認: `openspec list`が空
