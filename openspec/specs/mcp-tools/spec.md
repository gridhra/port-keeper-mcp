# mcp-tools Specification

## Purpose
AIエージェントが番号を覚えずにポートを引けるように、台帳をMCPで公開する。開示は最小にする。

## Requirements

### Requirement: Tool surface
MCPサーバーは既定で`current_context` `resolve_url` `resolve_port` `render_env` `status` `slot_new`を登録しなければならない（MUST）。`slot_release`と`list_all_projects`は設定で有効化した場合にのみ登録する。全ツールにannotations（readOnlyHint等）を付け、`openWorldHint`はfalse。

#### Scenario: Default registration
- **WHEN** 設定なしで`tools/list`を呼ぶ
- **THEN** 6ツールが返り、`slot_release`と`list_all_projects`は含まれない

### Requirement: Numbers only where asked
ポート番号を含む出力は`resolve_url`（URLの一部として）・`resolve_port`・`render_env`の3つに限り、いずれも問われたサービス分だけである（MUST）。`current_context`・`status`・`slot_new`・`list_all_projects`の出力に番号を含めてはならない。

#### Scenario: Context has no numbers
- **WHEN** `current_context`を呼ぶ
- **THEN** 出力にプール内の番号は現れない

### Requirement: Tools follow the caller's directory
各ツールは省略可能な`cwd`引数を受け、指定があればそのディレクトリからプロジェクトを解決しなければならない（MUST）。プロジェクト外のディレクトリではエラーを返し、`init`の実行を促してはならない（エージェントが無関係な場所にマニフェストを作る誘因になる）。

#### Scenario: Session moved to another 作業コピー
- **WHEN** サーバー起動時とは別の作業コピーのパスを`cwd`に渡す
- **THEN** その作業コピーのスロットで解決される

### Requirement: Cross-project lookup is explicit
別プロジェクトを引くときは`project`と`slot`の両方を要求しなければならない（MUST）。片方だけならエラー。

#### Scenario: Project without slot
- **WHEN** `resolve_url`を`project`だけ指定して呼ぶ
- **THEN** エラーとなり、`slot`が必要だと返す

### Requirement: Destructive tools dry-run without confirm
`slot_release`は`confirm`が真でない限り何も変更せず、何をするかを返さなければならない（MUST）。LISTEN中のサービスがある場合は拒否する。

#### Scenario: Release without confirm
- **WHEN** `slot_release`を`confirm`なしで呼ぶ
- **THEN** `dry_run: true`が返り、スロットは残る
