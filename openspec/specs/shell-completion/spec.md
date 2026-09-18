# shell-completion Specification

## Purpose
TBD - created by archiving change agent-ux-v0-2. Update Purpose after archive.

## Requirements

### Requirement: Completion scripts
`port-keeper completion zsh|bash|fish`は、そのシェル用の補完スクリプトを標準出力に出さなければならない（MUST）。未対応のシェル名はエラー。スクリプトは、第1語にサブコマンド、`--slot`と`--infra-from`と`slot rm`の引数にスロット名、`url`／`reassign`／`pin`／`unpin`の引数にサービス名、`env --format`にformat名、各コマンドの固定フラグを補完する。動的な候補は`port-keeper __complete <kind>`から取る。

#### Scenario: 3シェルのスクリプト
- **WHEN** `completion zsh`、`completion bash`、`completion fish`を実行する
- **THEN** それぞれ`#compdef port-keeper`、`complete -F _port_keeper port-keeper`、`complete -c port-keeper`を含むスクリプトが出て、`completion pwsh`は`unsupported shell`で終了コード1

### Requirement: Hidden candidate source never fails and never prints numbers
`port-keeper __complete <kind>`（kindは`command`／`format`／`slot-sub`／`service`／`slot`）は、候補を1行に1つ出力しなければならない（MUST）。プロジェクトの外、台帳を開けないとき、不明なkind、引数の過不足では、何も出力せず終了コード0で終わる（MUST）。`usage`には載せない。出力にポート番号を含んではならない（MUST NOT）。

#### Scenario: プロジェクトの中と外
- **WHEN** マニフェストのあるディレクトリで`__complete service`を実行する
- **THEN** マニフェストの順にサービス名が1行ずつ出る
- **WHEN** マニフェストの無いディレクトリで`__complete service`と`__complete slot`を実行する
- **THEN** 何も出ず、終了コードは0
