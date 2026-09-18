# Spec Delta

## ADDED Requirements

### Requirement: Machine-readable status
`status --json`は、1行のJSONで`project`、`slot`、`slot_source`、`services`（各要素は`service`、`port`、`proto`、`tier`、`state`、`shared`、`pinned`、任意の`listener`、`listener_cwd`（`hijacked`のときだけ）、`last_seen`（RFC 3339））、任意の`warnings`を返さなければならない（MUST）。`services`は空でも配列（`null`ではない）。警告はstdoutの別行ではなく`warnings`に入れる。CLIの`status`は表でも番号を出しているので、この出力に番号があってよい。MCPの`status`ツールはこの変更に含まれず、番号を出さないままである。

#### Scenario: 2サービスのスロット
- **WHEN** `web`と`api`を持つプロジェクトで`env`の後に`status --json`を実行する
- **THEN** 1行のJSONで、`services`が2要素、それぞれ`state`が`leased`、`port`がプールの中
