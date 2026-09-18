# Spec Delta

## ADDED Requirements

### Requirement: Self-check scope
`port-keeper doctor`は、台帳ディレクトリとファイルの権限、描画先（`render.dotenv_path`）がgitに追跡されず無視されていること、プールが一時ポートや慣習的な番号と重ならないこと、放置スロットと固定の件数、`port-keeper`という名前のプロセスが待ち受けていないこと、MCPクライアント設定にポートらしい番号やトークンらしい環境変数が無いこと、git追跡下の`.env`／`.env.*`が管理対象の変数を固定していないこと、描画した`.env.local`が台帳と合っていることを検査しなければならない（MUST）。各行は`[ok]`／`[warn]`／`[fail]`で始まる。

#### Scenario: すべて緑
- **WHEN** 問題の無いプロジェクトで`doctor`を実行する
- **THEN** すべての行が`[ok]`で、最後に`all green`が出て、終了コードは0

### Requirement: MCP client configuration scan reports no values
`doctor`は、Claude Code（`~/.claude.json`の`mcpServers`と`projects.*.mcpServers`）、リポジトリの`.mcp.json`、Cursor（`.cursor/mcp.json`、`~/.cursor/mcp.json`）、VS Code（`.vscode/mcp.json`の`servers`）、Claude Desktop、Codex（`~/.codex/config.toml`の`mcp_servers`）の設定ファイルのうち存在するものを読み、名前かcommandに`port-keeper`を含む項目だけを検査しなければならない（MUST）。argsかenvの値に1024〜65535の数があるか、envのキーが`token`／`secret`／`key`／`password`を含めば`[warn]`にする。警告にはファイルパスと項目名と見つかったものの種類だけを含め、設定の値やキー名を含めてはならない（MUST NOT）。無いファイルは黙って飛ばし、読めないファイルは`[warn]`で飛ばす。

#### Scenario: 番号とトークンがある項目
- **WHEN** `~/.claude.json`のport-keeper項目が`args: ["mcp", "3000"]`と`env: {"API_TOKEN": "x"}`を持つ
- **THEN** `[warn]`が2行（argsの番号、envのトークンらしいキー）出て、出力のどこにも`3000`と`API_TOKEN`が現れない

#### Scenario: 他のサーバーの項目
- **WHEN** 設定ファイルにport-keeper以外のサーバーの項目だけがあり、それが番号やトークンを持つ
- **THEN** 警告は出ず、`[ok] MCP client configs: N file(s) checked, 0 port-keeper entries`が出る

### Requirement: Tracked dotenv files that fix a managed variable
`doctor`は、マニフェストのディレクトリとリポジトリ最上位でgitに追跡されている`.env`と`.env.*`（描画先と、`.example`／`.sample`／`.template`で終わるものは除く）を読み、マニフェストのサービスか`[[derive]]`の環境変数を数値または`host:port`に設定している行があれば、ファイルと変数名だけを`[warn]`で報告しなければならない（MUST）。番号は出さない。

#### Scenario: 移行後に残った古い番号
- **WHEN** 追跡済みの`.env`に`WEB_PORT=3000`があり、マニフェストの`web`サービスの`env`が`WEB_PORT`
- **THEN** `[warn] .env sets WEB_PORT to a fixed value; which wins depends on your dotenv loader`が出て、`3000`は出ない

### Requirement: Exit code
`doctor`は、`[fail]`が1つでもあれば終了コード1で終わり、`[warn]`だけなら0で終わらなければならない（MUST）。

#### Scenario: 描画先が追跡されている
- **WHEN** `.env.local`が`git add`されている状態で`doctor`を実行する
- **THEN** `[fail] .env.local is tracked by git …`が出て、終了コードは1
