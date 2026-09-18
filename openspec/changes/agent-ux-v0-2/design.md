## Context

`v0.1.0`公開直後。M0（台帳コア）、M1（MCP）、M2（配布）が完了している。ここで直すのは新機能というより「入れたのに古い番号で動く」「環境が古いことに気づけない」という信頼に関わる穴と、導入と運用の摩擦。守る制約: MCPツールは8個から増やさない、`context`／MCP `status`／`slot_new`／`list_all_projects`に番号を出さない、待ち受け・デーモン・プロセス停止なし、テストで実ポートにbindしない。

## Goals / Non-Goals

Goals: 上のproposalの8項目。Non-Goals: 複数ポートのサービス、他クライアント向けフックアダプタ、Windows実機、MCPツール追加。

## Decisions

- **driftは描画物の突合で検出する。`manifest_hash`列は追加しない。** 設計書§3.11のスキーマにあった`projects.manifest_hash`は実装されておらず、あってもマニフェストの変更しか捉えない。`.env.local`のブロックを台帳から描画し直して比べれば、サービス追加、`reassign`、手編集、削除、別スロット向けの描画をすべて拾える。スキーマ変更（`SchemaVersion`の更新と移行）も不要。検討した代替: `manifest_hash`列（スキーマ変更が要り、検出範囲が狭い）
- **driftの判定順**: スロット未作成 → リースの無いサービス（`Resolved`はリース無しを拒否するので、台帳の`Leases`で先に調べる。`--infra-from`で共有するinfraサービスは自スロットにリースが無くて正常なので除く）→ ファイル無し → ブロック無し → ヘッダの`project/slot`不一致 → 本文不一致。理由文は相対パスと名前だけ
- **フックでは`EnvText`（内部で未リースを貸す）の後にdriftを評価する**。先に評価すると「リースが無い」がその場で解消されて古い案内になる。CwdChangedの`systemMessage`は最初の一文だけを出していたので、driftの指示文を短い形にも足した（`printHook`に`short`引数）
- **`doctor`のMCP設定検査は`internal/doctor`パッケージに置き、`Finding{Level, Msg}`を返す**。台帳に依存せずtempディレクトリだけで単体テストできる。`cmdDoctor`は既存の`report`クロージャと文言を維持し、findingsを流し込むだけ。ホームディレクトリは引数（テストはtemp）
- **検査対象の設定ファイル**: `~/.claude.json`（`mcpServers`と`projects.*.mcpServers`）、`~/.cursor/mcp.json`、`~/.codex/config.toml`、Claude Desktop（darwin／linux／windowsで場所が違う）、リポジトリの`.mcp.json`／`.cursor/mcp.json`／`.vscode/mcp.json`（キー`servers`）。名前かcommandに`port-keeper`を含む項目だけ。他のサーバーの設定は見ない（他人の秘密を読み込む理由が無い）
- **値を出さない**。警告文はファイルパス・項目名・「argsに番号」「envにトークンらしいキー」までで、値もキー名も出さない。追跡済みdotenvの警告も変数名だけ。由来: 台帳を`lsof`以上の地図にしない規律（§5.1）。`doctor`の出力はissueに貼られる
- **追跡済みdotenv検査から`.example`／`.sample`／`.template`を除く**。ローダーが読まないので誤検知源。実ファイルはgitの`ls-files`で列挙し、macOSの`/var`→`/private/var`のようなsymlinkで同じファイルを二重に数えないよう実パスで重複除去する（テストで実際に踏んだ）
- **`doctor`は`[fail]`で終了コード1、`[warn]`は0**。`[fail]`は追跡済みの描画先と自己待ち受けの2つで、どちらも「止めて直す」状態。`[warn]`を0に留めるのは、フック等から安全に呼べるようにするため
- **`status --json`は番号を含む**。CLIの`status`の表が既に出しているので規律に触れない。MCPの`status`ツールは変えない。`--json`時は`warn()`をstdoutに出さず`warnings`配列へ入れ、1行のJSONにする
- **`slot new --from-branch`は`git symbolic-ref --short -q HEAD`を使う**。`rev-parse --abbrev-ref HEAD`は未コミットの新規リポジトリで失敗し、detachedで文字列`HEAD`を返す。正規化: 小文字化、`[a-z0-9]`以外の連続を1つの`-`に、前後の`-`除去、63バイトで切る。スロット自動作成の方針（フックは案内だけ）には触れない
- **補完は静的スクリプト＋隠し`__complete`**。`__complete`は`Main`で`run`の前に分岐する（`run`は台帳を開けない場合に`port-keeper: …`を出して1で終わるが、補完中にそれが出てはいけない）。どんな失敗でも無出力・0。候補は名前だけ。zsh／bashは`--slot <name>`を読み飛ばしてからサブコマンドを探す
- **エージェントevalは手動**。`claude --bare`（フック・CLAUDE.md・キーチェーンを読まない。`ANTHROPIC_API_KEY`必須）で、temp台帳とdemoプロジェクトに対して3シナリオ。`--max-turns`は2.1.275のローカルhelpに無いので渡さず、`--max-budget-usd`で止めて往復は事後に数える。stream-jsonの項目名は初回の本番で確認する。CIでは`--list`（何も作らない）と`--dry-run`（組み立てとコマンド行の印字）だけ
- **`docs/examples/`は英語のみ**。翻訳はREADMEだけ。README同期検査は、fence内を除いた`## `／`### `の数、fence行数、CLI表とMCPツール表の各行の先頭セルのバッククォート部分の並びを比べる（「or」のような訳される語は比べない）

## Risks / Trade-offs

- driftの突合はマニフェストの読み込みと台帳の読み取りが要り、`context`とフックのたびに走る。読み取りだけで小さいが、失敗しても`context`を失敗させない（無視する）
- `doctor`の終了コード変更は、`doctor`の終了コードに依存していた利用者（いれば）に影響する。`[warn]`は0のままなので、影響は`[fail]`の2条件に限られる
- evalは非決定的。閾値は緩め（s1は2往復、s2は3、s3は4）で、FAILは「文言を直して再実行」の合図

## Migration

不要。台帳スキーマは1のまま。`.env.local`の形式も変わらない。
