# ledger-confidentiality Specification

## Purpose
台帳は「この端末で何が何番で動いているか」の地図であり、サービス列挙の結果そのもの。保管・開示・保持を最小にする。

## Requirements

### Requirement: Storage permissions
台帳ディレクトリは0700、台帳ファイル（WAL側ファイル含む）は0600で作成しなければならない（MUST）。`doctor`はこれを検査し`--fix`で直す。

#### Scenario: Fresh ledger
- **WHEN** 台帳を初めて開く
- **THEN** ディレクトリは0700、ファイルは0600で作られ、`doctor`は緑を報告する

### Requirement: No listener, no daemon
port-keeperはTCPでもUnixソケットでも待ち受けてはならない（MUST NOT listen）。実在確認のbindは即座に閉じ、acceptしない。

#### Scenario: Probe leaves nothing open
- **WHEN** `status`が実在確認のためにbindを試みる
- **THEN** 試行直後にソケットは閉じられ、port-keeperがLISTENしているポートは存在しない

### Requirement: Nothing beyond names and numbers
台帳はパスワード・トークン・接続文字列の認証部を保持する列を持ってはならない（MUST NOT）。監査表には操作種別と名前だけを記録し、番号を記録しない。

#### Scenario: Audit entry
- **WHEN** スロットを作成・解放する
- **THEN** 監査表には操作種別と`project/slot`の名前だけが残り、ポート番号は残らない

### Requirement: Minimal disclosure to agents
MCPの既定の開示範囲はカレントプロジェクト・カレントスロットである（MUST）。マニフェストの`label`は64文字で切り、構造化出力のフィールド値としてだけ返す。

#### Scenario: Long label from a cloned repository
- **WHEN** `label`が64文字を超える
- **THEN** 64文字に切られ、`label`フィールドの値としてのみ現れる
