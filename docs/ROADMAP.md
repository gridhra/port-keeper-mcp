# ROADMAP — 「ローカルのポートを、人もエージェントも意識しない」状態へ

作成日: 2026-09-17。設計本文は[DESIGN.md](DESIGN.md)。本書は順序と規律だけを持つ。

## ゴール判定

次の3つが同時に成り立てば「完備」とみなす。

1. 任意のプロジェクトで、スロットを何個作ってもポートが衝突しない（プール容量以外の上限がない）
2. 人もエージェントも、URLを得るために番号を思い出す・探す・計算する操作を一度もしない
3. 台帳が`lsof`以上の地図になっておらず、`doctor`が緑

## フェーズ

### M0 — 台帳コア（CLIのみ）— 完了（2026-09-17）

スキーマ、プール、連続ブロック割当、bindによる実在確認、`init / slot / env / url / status / gc / pin / unpin / doctor`。
完了条件: 30サービスのプロジェクトでスロット5以上が立つ。2プロジェクトが同じ番号を`pin`すると2つ目が所有者付きで拒否される。property testで「有効なリース同士は重ならない・全てプール内」が通る。

### M1 — MCP — 完了（2026-09-17）

`port-keeper mcp`（stdio）と8ツール、annotations、`structuredContent`、Claude Codeの登録・フック雛形。
完了条件: 「shopのslot5のadminのURL」が1ツール呼び出しで返る。`current_context`の出力に番号が無いことをスキーマで検証。

### M2 — 配布 — 完了（2026-09-18）

`v*`タグのpushで動くリリース（GitHub Actionsの`release.yml`とGoReleaser。macOS／Linux／Windows、arm64／amd64の6アーカイブ、`checksums.txt`、ビルド来歴の証明、公開後に変更できないRelease）、インストールスクリプト（`scripts/install.sh`／`install.ps1`。チェックサムの照合が必須で、`sudo`を使わない）、台帳のスキーマの版、README 3言語の導入手順、メンテナ向けの`RELEASING.md`、Glama（MCPサーバーの登録・評価サイト）への提出前検証（`scripts/glama.sh`）。
`v0.1.0`を公開し、Glamaに登録した。awesome-mcp-serversへの掲載PRは提出済み（#14635）。手順と記録は`RELEASING.md`。
完了条件: 新しい端末で5分で導入でき、`doctor`が緑。

当初の案から変えた点（理由は`docs/DESIGN.md`の§9.2）:

- **npmラッパー（`npx port-keeper-mcp`）は作らない**。フックと人の操作がPATH上の`port-keeper`コマンドを必要とするので、都度起動ではなく常設インストールを導入経路にした。同じ作者のatx-mcpで、npmの公開にメンテナの手作業が多かったことも理由である
- **コンテナイメージは配らない**（READMEのNon-goals）。したがってMCP公式レジストリにoci形式では登録しない

### M2.5 — Agent UXの仕上げ（v0.2.0）— 実装完了（2026-09-18。リリースは未実施）

`v0.1.0`を使って分かった「次に躓く点」をまとめて直した。内容: `doctor`のMCP設定検査（値は出さない）と追跡済み`.env`の検査、書き出した`.env.local`の古さ検出（`context --json`の`env_stale`、Claude Codeフック、`doctor`）、`status --json`、`slot new --from-branch`、`completion zsh|bash|fish`、`docs/examples/`（mise／direnv／docker compose／Vite／Playwright／リバースプロキシ／monorepo。英語のみ）、README 3言語の同期検査（`scripts/readme_sync_check.sh`、CI）、エージェントeval（`scripts/agent_eval.sh`、手動のリリースゲート）。決定と理由は`docs/DESIGN.md`の§9.3。
完了条件: `sh scripts/agent_eval.sh`の3シナリオが往復数と番号露出の上限内で通る。

### M3 — 需要駆動

- Windowsの実在確認とパス規約、インストールスクリプトのWindows実機での確認
- **MCP公式レジストリへの登録**。レジストリが受け付ける形式のうち、npmとoci（コンテナ）はM2の節に書いた理由で採らない。mcpb形式（GitHub Releaseに置くまとめファイル）も採らない。調査（2026-09-18）で分かったこと: (1) `.mcpb`を導入できるクライアントはClaude Desktop（macOSとWindows）だけで、Claude Code、VS Code、Cursorには導入経路が無い。VS Codeは、mcpbしか持たないサーバーを一覧から落とす。(2) Claude Desktopには「開いているプロジェクトの作業ディレクトリ」が無く、起動されるサーバーの作業ディレクトリも文書化されていない。port-keeperはクライアントの作業ディレクトリからプロジェクトとスロットを解決するので、コンテナと同じ理由で成立しない。(3) コンパイル済みバイナリを同梱する種別（`server.type: "binary"`）は、macOSのClaude Desktopでは展開時に実行権限が落ちて起動しない不具合が未修理である（`modelcontextprotocol/mcpb`のissue #294）。(4) `.mcpb`の中のバイナリはPATHに入らないので、npmラッパーと同じく、版の違う2本のバイナリが1つの台帳を共有する。なお、当初の懸念だったCPU（amd64／arm64）の区別は、`server.json`の`packages`にOSとCPUごとの`.mcpb`を複数並べる方法で解決できる（Goの実例がレジストリに複数ある）ので、見送りの理由ではない。性質が合う形式は、レジストリに提案されている`go`形式（`go install`できるGoモジュールを登録する。バイナリはPATH上に常設される）で、2026-09-18時点では未マージである（`modelcontextprotocol/registry`のissue #1307とPR #1321）。**再検討の条件: PR #1321がマージされたとき。** `server.json`は、パッケージの記述を持たない下書きのまま置いてある
- Homebrew tap（tap用の別リポジトリと、そこへ書き込む長期トークンの管理が要るので、要望が出てから）
- Streamable HTTP（DESIGN §5.3の条件を全て満たす場合のみ）

## Agent UXの規律（MCPツール面の洗練）

1. **ツール面は小さく保つ**。8ツールから増やさない。新しい能力は描画format・マニフェスト項目・`doctor`検査として足す
2. **番号を含む出力は3ツールだけ**（`resolve_url`、`resolve_port`、`render_env`。いずれも問われたサービス分のみ）。他は名前で話す。会話ログに番号が溜まらないようにする
3. **既定の開示範囲はカレントプロジェクト**。他プロジェクトは`project`引数を明示させる。全体一覧と破壊系は設定で有効化した場合のみ登録
4. **エラーが教師**。存在しないスロット名には存在する名前の列挙と作り方を返し、1往復で自己修復できるようにする
5. **エージェントevalをリリースゲートに**。「slot 3のadminを開いて」「hotfix用に環境を増やして」等の実務タスクをMCP接続済みエージェントに解かせ、往復数と番号の露出回数を測る。ツール説明・エラー文言の変更はこのevalで回帰確認する。実装は`scripts/agent_eval.sh`（APIを呼ぶので、CIではなくタグを打つ前に手で回す。手順は`RELEASING.md`）

## 横断的な規律

- **非ゴールを守る**（DESIGN §1.2）。プロキシ・プロセス管理・デーモン・台帳共有・秘密情報・プール外自動割当は、要望が来ても理由に対する反論が無い限り入れない
- **台帳をlsof以上の地図にしない**。新しい列や新しい返却項目を足すときは「lsofで取れる情報＋名前」の範囲か、SECURITY.mdのIn scopeを増やさないかを確認する
- **常駐しない**。常駐が要る機能は別コンポーネントに切り出すか、作らない
- **描画の互換**。`dotenv`のマーカー形式と各formatの出力はゴールデンテストで固定し、変更は破壊的変更として扱う
