## Context

設計の全文は [docs/DESIGN.md](../../../docs/DESIGN.md)。本書は実装に直結する決定と、実装中に確定した差分だけを持つ。制約: 常駐しない、待受を持たない、台帳は`lsof`以上の地図にしない、番号をマニフェストに書かない。

## Goals / Non-Goals

**Goals:**
- スロット数の上限をプール容量だけにする
- 名前からURLを1往復で返す（CLI・MCP）
- 台帳の機微性を運用で守れる形にする（権限・開示範囲・自己点検）

**Non-Goals:**
- 名前解決プロキシ、プロセスの起動・停止、デーモン、台帳の同期、秘密情報の保持、プール外の自動割当（DESIGN.md §1.2）

## Decisions

- **SQLiteは単一接続（`SetMaxOpenConns(1)`）＋`_txlock=immediate`**。複数プロセス間の排他はSQLiteのロックと`busy_timeout`に任せ、プロセス内は接続1本で直列化する。副作用として、トランザクションの内側から`Ledger`（別接続）を読むとデッドロックするため、トランザクション内の読み取りは必ず`Tx`のメソッドを通す（`leaseSource`インターフェース）。実装中に実際にデッドロックを踏んで確定した規則
- **固定（pin）はブロックを持たない**。設計書の初版にあった`blocks.kind`列は廃止し、固定は`leases.pinned_at`と`pin_reason`だけで表す。プール内の固定番号は割当時に`leases.port`を見て避ける
- **ブロック内の空きオフセット選択でも実在確認する**。`nextFreePort`は台帳外のプロセスがLISTENしているポートを飛ばす。`reassign`はこの仕組みに「直前の番号」を加えて別番号へ移す
- **実在確認はSO_REUSEADDRを外したbind試行＋connect試行**。BSD系ではSO_REUSEADDR付きだとワイルドカードLISTENに対して特定アドレスのbindが通ってしまうため
- **`hijacked`はLISTENプロセスのcwdが作業コピー外のときだけ**。cwdが取れない・コンテナランタイム（docker/podman/colima等）は`active`扱い。誤検知より見逃しを選ぶ
- **CLIのフラグは位置引数の前後どちらでも受ける**（`parseMixed`）。標準`flag`は最初の位置引数で解析を止めるため、`slot new 2 --infra-from 1`が黙って無視される事故があった
- **`env --if-present`**。マニフェストの無いディレクトリではexit 0で何もしない。Claude CodeのSessionStartフックに置く前提
- **MCPの入力スキーマで省略可能な引数は`omitempty`を付ける**。SDKが入力を検証するため、`confirm`を必須にすると「確認なしのドライラン」が呼べなくなった

### セルフレビュー（2026-09-17、コードと文書の2系統）で確定した決定

- **dotenv描画は改行を含む値を必ず引用する**。マニフェスト（cloneした他人のリポジトリ由来でありうる）の`host`や`derive`に改行を仕込んでも、`.env.local`に別の`KEY=VALUE`行を注入できない。マニフェスト側でも複数行の`derive`/`label`を拒否
- **`render.host`はループバック名に限定**（`localhost`／`127.0.0.1`／`::1`／`*.localhost`）。`url --open`が外部URLを開く経路を塞ぐ
- **他スロットのブロック内の番号は`--force`なしに固定できない**。固定できてしまうと、ブロックの持ち主が次にサービスを足したときにUNIQUE違反で`env`が恒久的に失敗する。加えて空きオフセットの選択時に全リースと突合し、プール外・deny・台帳外LISTENも飛ばす
- **MCPの`status`は本当に読み取り専用**（`last_seen_at`を触らない）。触るのはCLIの`status`だけ
- **名前なしの`slot_new`は、作業コピーが既にスロットに紐づいていればそのスロットを返す**。エージェントの再試行でスロットが増え続けない
- **固定済みサービスをマニフェストから消すと同期を拒否**し、`unpin`を案内する。理由付きの例外を無言で消さない
- **`env`は描画直後に`hijacked`を再確認して警告**する（DESIGN §3.9の緩和策）
- **CLIは`--`終端を認識し、引数を取らないコマンドの余分な位置引数はエラー**（`port-keeper env json`が黙ってdotenvを書く事故を防ぐ）
- **設定ファイルの未知キーはエラー**。`doctor`の自己待受検査は`lsof`で実検査。SessionStart用に`hook claude-session-start`を追加
- **複数OSプロセスの同時実行テスト**をテストバイナリの再実行で追加（8プロセスが`slot new`、8プロセスが`env`）
- **フック形式は公式ドキュメントで確認**（`CLAUDE_ENV_FILE`＝`export`文の追記、SessionStart＝`hookSpecificOutput.additionalContext`）。`CwdChanged`でも`CLAUDE_ENV_FILE`が使えると分かったので、フックはstdinのJSONからイベント名と移動先ディレクトリを読み、両イベントを1コマンドで扱う

## Risks / Trade-offs

- 確認から起動までの隙間の競合はゼロにできない（プール位置と`hijacked`報告で緩和）
- Go 1.25が必要（`modernc.org/sqlite`と`go-sdk`の要求）。`GOTOOLCHAIN`既定で自動取得される
- Windowsは未検証（bindの挙動、`lsof`不在）
