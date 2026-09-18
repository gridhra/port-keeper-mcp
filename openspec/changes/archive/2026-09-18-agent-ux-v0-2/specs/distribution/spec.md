# Spec Delta

## ADDED Requirements

### Requirement: README mirrors are checked in CI
`scripts/readme_sync_check.sh`は、`README.md`、`README.ja.md`、`README.zh-CN.md`について、コードブロックの外にある`## `と`### `の数、コードブロックの区切り行の数、CLI表とMCPツール表の各行の先頭セルにあるバッククォートで囲まれた部分の並びを比べ、違いがあれば表と差分を出して終了コード1で終わらなければならない（MUST）。CIの必須ジョブ`test`で実行する。

#### Scenario: 表の行を1言語だけ足した
- **WHEN** `README.md`のCLI表に行を足し、`README.ja.md`には足さずに実行する
- **THEN** 足りない行が差分として出て、終了コードは1

### Requirement: Agent eval is a manual release gate
`scripts/agent_eval.sh`は、一時的な台帳とdemoプロジェクトに対して、MCPを接続したClaude Code（`claude --bare -p`）に3つのシナリオ（`slot 3`の`admin`のURLを聞く、未紐づけのworktreeに環境を用意させる、`dev.sh`を起動させる）を与え、往復数、呼ばれたツール、最終回答とBashコマンドに漏れたプール範囲の番号を数えて表にし、上限を超えたシナリオがあれば終了コード1で終わらなければならない（MUST）。APIを呼ぶのでCIでは実行せず、タグを打つ前に手で回す。`--list`（何も作らない）と`--dry-run`（組み立てとコマンド行の印字。APIは呼ばない）はCIで実行する。

#### Scenario: CIでの煙テスト
- **WHEN** `sh scripts/agent_eval.sh --list`と`PK_EVAL_BIN=./port-keeper sh scripts/agent_eval.sh --dry-run`を実行する
- **THEN** どちらもAPIキー無しで終了コード0

### Requirement: Examples are English only
`docs/examples/`は英語だけで書き、翻訳してはならない（MUST NOT）。翻訳するのはREADMEだけである。例にはポート番号を書かず、環境変数名とサービス名で書かなければならない（MUST）。

#### Scenario: 番号の無い例
- **WHEN** `docs/examples/`を4〜5桁の数で検索する
- **THEN** コンテナ内部のポート（`5432`など、ホストの番号ではないもの）以外は見つからない
