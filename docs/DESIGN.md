# port-keeper-mcp — 要件定義・設計書

作成日: 2026-09-17 / ステータス: M0（台帳コアCLI）・M1（MCP）実装済み。本文§1〜§8は当初設計、実装で確定した差分は§9。

ローカルホストの開発用ポートを**台帳で中央管理し、名前で引ける**ようにするCLI兼MCPサーバー。Go製。
「ポート番号は本来なんでもよい」という前提に立ち、人もAIエージェントも番号を**意識せずに**済む層を提供する。番号を**見ない**ことは目的にしない（§1.2）。

---

## 1. プロダクト定義

### 1.1 ゴール

- プロジェクトがサービス名と環境変数名だけを宣言すれば（マニフェスト。番号は書かない）、環境ごと（スロット）に衝突しないポートの組が貸し出され、`.env`等に描画される
- 複数プロジェクト・複数スロットを横断した衝突判定を台帳が担い、人が覚えない
- `port-keeper url shop/5/admin` またはMCPの`resolve_url`で、ポート込みのURLが1往復で返る
- 台帳は「この端末で何が何番で動いているか」の地図そのものなので、ローカル限定・最小開示・最小保持で運用する（§5）

### 1.2 非ゴール

将来の要望として必ず出るものなので、READMEのNon-goalsとissueテンプレートでも先回りして示す。

- **名前解決プロキシ**（`http://admin.5.shop.localhost`を実ポートへ転送する常駐プロセス）。目的は「番号を意識しない」であって「番号を見ない」ではない。名前からポート込みURLが引ければ足りる。プロキシは常駐が止まると全スロットへ到達できない単一障害点になり、80番待受の権限やTLS転送を持ち込み、§3の「常駐しない」方針を壊す。ブラウザの保存領域（Cookie・localStorage）はポートを含むoriginごとに分かれるので、番号が違うこと自体がスロット分離の利点であり、名前で隠す理由がない。欲しい人は既存のプロキシ（portless、localias、devenv）に`port-keeper env --format json`を渡せばよい
- **サーバーの起動・監視・停止**。ポートを予約したサービスを誰がいつ起動するかは、mise／direnv／docker compose等の利用者側の道具の仕事。プロセスを殺す機能も持たない（§5.3）
- **常駐デーモン**。SQLiteの直列化で要件を満たせる（§3.2）
- **台帳の共有・同期**。共有するのはマニフェストだけ
- **秘密情報の保持**。パスワード・トークン・接続文字列の列を作らない
- **プール外の自動割当**。例外は`pin`のみで、横断調停・理由必須（§3.6）
- **クラウド・コンテナ内のポート管理**。対象はローカルホストのみ

### 1.3 競合状況（2026-09調査）

17件を実体確認した（実行はしていない）。要件（申請と記録／複数スロット／規則の配布／利用者側でのenv操作／慣習的ポート回避／MCP）を全部満たし、かつ単機能で導入できるものは無い。

| 分類 | 代表 | 足りないもの |
| --- | --- | --- |
| 台帳あり・スロット対応・MCPなし | BindPort（Rust、SQLite台帳、作業コピー識別、29000番台プール） | MCP層。台帳の機微性への配慮 |
| 台帳あり・MCPあり | Port Daddy（TS、SQLite、MCPツール180本） | AIエージェント調停基盤の一部。ライセンスFSL-1.1、単機能導入に不向き |
| MCP純粋実装 | mcp_portman（Python、JSON台帳） | スロット概念なし、env受け渡しなし、実在確認なし |
| 名前解決プロキシ | portless、localias | 台帳・所有者記録なし。直交するので併用先 |
| 部品ライブラリ | get-port / detect-port | 空き探索のみ |

流用する発想: bind試行による実在確認（Port Authority、BindPort）／作業コピーを識別子に含める（BindPort、Outport）／`.env`へのテンプレート展開（BindPort、devports）。

### 1.4 背景（抽象化した事象）

- ある中規模Webプロジェクトは1環境あたり約30ポートを使い、並列環境を「基準値＋(スロット−1)×1000」で導出していた。サービス間の基準値差がちょうど4000のペア（例: 3001と7001）があるため、スロット5がスロット1と衝突し、上限が4に固定されていた。ポート不足ではなく式の副作用
- 端末上には複数プロジェクトが並走し、それぞれ独自の千の位でスロットを切っていた。横断の衝突は人の記憶に頼っていた
- OSや常駐アプリが慣習的な番号を先に握る（実測: macOSのControlCenterが5000と7000をLISTEN）
- 人もAIも「管理画面はどの番号だったか」を覚える必要があり、打ち間違いや、番号をチャットに貼る漏えい経路が生まれていた

---

## 2. 技術選定

### 2.1 言語・配布

- **Go**。マルチプラットフォームの単一バイナリをクロスビルドするため。SQLiteはcgo不要の`modernc.org/sqlite`
- 配布はGoReleaser（macOS／Linux／Windows、arm64／amd64）＋インストールスクリプト＋`go install`。当初はHomebrew tapとnpmラッパー（`npx port-keeper-mcp`）も挙げていたが、npmラッパーは作らないと決め、Homebrew tapは需要が出るまで保留にした（2026-09-18。理由は§9.2）
- MCP SDKは`github.com/modelcontextprotocol/go-sdk`。stdioのみ。annotations（readOnlyHint等）の実機動作は未確認（§8）

### 2.2 台帳

- SQLite 1ファイル。`$XDG_STATE_HOME/port-keeper/ledger.sqlite`（既定`~/.local/state/port-keeper/`）。WALモード。ディレクトリ0700、ファイル0600
- 全書き込みは`BEGIN IMMEDIATE`で直列化。複数のCLI／MCPプロセスが同時に走っても重複割当しない。`leases.port`はUNIQUE

---

## 3. アーキテクチャ

### 3.1 3層

| 層 | 役割 | 形 |
| --- | --- | --- |
| 台帳コア | リースの割当・返却・実在確認・描画 | `port-keeper` CLI。呼ばれたときだけ動く |
| MCPアダプタ | エージェントに台帳を名前で引かせる | `port-keeper mcp`（stdio）。クライアントがセッションごとに子プロセスとして起動 |
| 利用者側の受け口 | 割当を環境変数として各ツールに渡す | `.env`ブロック／shell export／mise・direnv用出力／Claude Codeの`CLAUDE_ENV_FILE` |

### 3.2 常駐しない理由

(1) 常駐すると台帳を読める経路がファイル権限だけでなくソケット／HTTPにも広がる。(2) ポート管理ツールがTCPで待ち受けると、それ自体が1ポートを占有し、台帳を握るサービスが外から叩ける形になる。(3) 起動しっぱなしのプロセスは更新・再起動・ログの面倒を増やす。要件は全て「呼ばれたときに答える」で満たせる。

### 3.3 識別子と割当

- リースの鍵は`project/slot/service`の3段
- スロット作成時にプールから**連続ブロック**（既定32ポート、マニフェストで変更可）をfirst-fitで貸す。サービスはマニフェストの宣言順でブロック内のオフセットを取る。超えたら2ブロック目
- 同じスロットは解放されるまで同じブロックを保つ（再起動をまたいで安定）
- なぜ連続ブロックか: 導出式方式はサービス間の基準値差がスロット幅の整数倍になると衝突する（§1.4）。ブロック丸ごとなら距離はブロック内に閉じる。`lsof`でも1スロットの口が固まって並ぶ
- なぜ決定論ハッシュにしないか: 衝突時の再解決に結局台帳が要る。番号が外から推測できる

### 3.4 プールと除外

`~/.config/port-keeper/config.toml`:

```toml
[pool]
ranges = [[20000, 31999]]          # 12,000ポート = ブロック32なら375スロット分
deny_ports = [27017, 28015, 29092] # プール内でも避けたい慣習的番号
```

上限31999: OSの一時ポート範囲（macOS 49152〜65535、Linux 32768〜60999）を避ける。下限20000: 3000／5000／5173／8000／8080／9000／9229等の慣習的番号と、macOSが握る5000／7000を避ける。

### 3.5 スロットの決定と階層

- スロットは (1) 明示引数`--slot` → (2) カレントのgit worktreeルートに紐づくスロット → (3) 環境変数`PORT_KEEPER_SLOT` → (4) マニフェストの`slot_default` の順で決める。環境変数を紐づけより下に置くのは、port-keeper自身がフックや`env --format export`で`PORT_KEEPER_SLOT`を書き出すため。シェルに残った古い値が、`slot new`で紐づけたばかりのスロットを隠してはならない（独立レビューで実機再現された事故）。環境変数が紐づけと食い違って無視されたときは、`env`／`status`／`context`が警告を出し、MCPの`current_context`とフックの案内文にも含める
- 作業コピーに紐づくスロットが無く、`slot_default`のスロットが別の作業コピーに紐づいている場合は「未紐づけ」とし、`env`／`url`／`status`／MCPツールは既定スロットの番号を渡さず`slot new`を案内する。`--slot 1`の明示なら共有できる。`git worktree add`直後の`env`が本線の番号を配ってしまう事故（独立レビューで実機再現）を防ぐ
- サービスには`tier`（`app`／`infra`）。`slot new --infra-from <slot>`で`infra`層を別スロットから参照する（DBやメールモックを複数のアプリ環境で共有）。参照されているスロットは`--cascade`なしに解放できない

### 3.6 固定番号（pin）— 横断調停付きの例外

既存プロジェクトの移行で「メインの環境だけ今までの番号を保ちたい」は必ず出る。禁止はしないが、各プロジェクトが自分のスロット1を固定し始めると横断管理が人の記憶に戻るので、次の圧をかける。

- 固定も台帳の`port UNIQUE`の下に置く。Aが3000を固定済みならBの`pin 3000`は所有者を示して拒否する
- 固定は1プロジェクト1スロット（`slot_default`）まで。`--reason`必須。固定前に実在確認
- `doctor`が件数と経過日数を出す（90日超は黄色）。`init`雛形に固定手順は載せない。目標状態は固定ゼロ

### 3.7 マニフェスト（commitする。番号は書かない）

`port-keeper.toml`。`[project]`（name、block_size、slot_default）、`[[service]]`（name、env、proto、tier、label）、`[[derive]]`（番号から組み立てる環境変数。`${port.x}` `${url.x}` `${slot}` `${slot.infra}` `${project}` `${block.base}`）、`[render]`（dotenv_path、dotenv_marker、host。hostは全URLに使う。既定`localhost`）。具体例はREADME。

### 3.8 リースの状態

| 状態 | 意味 |
| --- | --- |
| `leased` | 台帳上は貸出中。LISTENは問わない |
| `active` | 直近の`status`／`env`でLISTENを確認 |
| `stale` | 30日LISTENなし。`gc`が候補提示。自動解放しない |
| `hijacked` | LISTENプロセスの作業ディレクトリが作業コピー外（コンテナ系は除く）。`reassign <service>`でそのサービスだけ別ポートへ |

### 3.9 実在確認と競合

IPv4とIPv6の両ループバックにbind試行し、さらにconnect試行で二重に確かめる（bindはSO_REUSEADDRを外しても、権限やIPv6無効などの理由で「使用中」と区別できない失敗を返しうるため。connectは閉じたポートには即時失敗し、開いていれば1接続を作って即閉じる）。`lsof`は所有プロセス名の表示にだけ使う。「確認から起動までの隙間」に台帳外プロセスが取る競合はゼロにできない（BindPortも未解決と明記）。緩和策は、プールを慣習的番号と一時ポートから離すこと、`env`直前に再確認して`hijacked`を報告すること。

### 3.10 CLI

| コマンド | 役割 |
| --- | --- |
| `init` | マニフェスト雛形、`.gitignore`追記 |
| `slot new [name] [--infra-from]` / `slot ls` / `slot rm` | スロットの作成・一覧・解放 |
| `env [--format dotenv\|export\|json\|mise\|direnv\|claude-env]` | 割当と導出値の出力。`dotenv`はマーカーブロックを冪等更新 |
| `url <service>` / `url <project>/<slot>/<service>` `[--open]` | URLを1行 |
| `status` / `gc` / `doctor` | 突合・回収候補・自己点検 |
| `pin <service> <port> --reason` / `unpin` | 固定の登録・解除 |
| `mcp` | stdio MCPサーバー |

### 3.11 SQLiteスキーマ

```sql
projects(id, name UNIQUE, manifest_hash, created_at)
slots(id, project_id, name, root_path, infra_from_slot_id NULL, created_at, released_at NULL,
      UNIQUE(project_id, name))
blocks(id, slot_id, base, size, kind CHECK(kind IN ('pool','pinned')), created_at)
leases(id, slot_id, service, port UNIQUE, tier, proto, pin_reason NULL, pinned_at NULL,
       last_seen_at NULL, UNIQUE(slot_id, service))
audit(id, at, actor CHECK(actor IN ('cli','mcp')), op, target)   -- 番号は記録しない
```

### 3.11a クライアント不可知の契約

核（台帳・割当・描画・MCP）は、どのエージェントやシェルから使われるかを知らない。クライアント向けの契約は2コマンドだけ: `port-keeper context --json`（プロジェクト・スロット・この作業コピーが使える状態か・次に何をすべきか。番号は含まない）と `port-keeper env --format export --if-present`（環境変数。プロジェクト外では無出力）。クライアント固有の作法（Claude Codeのフックがstdinに渡すJSON、`CLAUDE_ENV_FILE`、返すJSONの形）は、この2つを呼ぶだけのアダプタ（`internal/cli/hook_claude.go`）に閉じ込め、核には持ち込まない。他のクライアントは同じ形でアダプタを足す。

### 3.12 リポジトリ構成

```
cmd/port-keeper/   internal/{ledger,probe,manifest,render,mcp}/
docs/{DESIGN,ROADMAP}.md   README.md   SECURITY.md   server.json   npm/   .github/
```

---

## 4. MCPツール仕様（v1）

カレントディレクトリからプロジェクトとスロットを解決し、**既定ではそのプロジェクトの情報しか返さない**。

| ツール | 役割 | annotations |
| --- | --- | --- |
| `current_context` | project、slot、サービス名一覧（番号なし） | readOnly:true |
| `resolve_url` | サービスのURL 1本。他プロジェクトは`project`必須 | readOnly:true |
| `resolve_port` | 番号1つ | readOnly:true |
| `render_env` | 現在スロットの環境変数の組。未リースのサービスにはポートを貸す | readOnly:false, destructive:false, idempotent:true |
| `status` | 各サービスのleased/active/stale/hijacked | readOnly:true |
| `slot_new` | スロット作成 | readOnly:false, destructive:false, idempotent:true（同名なら既存を返す） |
| `slot_release` | 解放。LISTEN中は拒否。`confirm:true`必須 | destructive:true。設定で有効化した場合のみ登録 |
| `list_all_projects` | 全プロジェクトとスロット名（番号なし） | readOnly:true。設定で有効化した場合のみ登録 |

全ツール`openWorldHint:false`。

### 4.1 返却パターン（トークン規律・開示規律）

- `structuredContent`＋`outputSchema`で機械可読に返し、`content`は1行の要約
- 番号を含む出力は`resolve_url`（URLに番号を含む）・`resolve_port`・`render_env`の3つだけで、いずれも問われたサービス分のみ。`current_context`・`status`・`slot_new`・`list_all_projects`は名前だけで話す。会話ログに番号が残る面積を減らす
- マニフェストの`label`は64文字で切り、`"label"`フィールドの値としてだけ返す（他人のリポジトリのマニフェストに指示文が仕込まれてもデータとして隔離）
- Elicitation（サーバーからの確認要求）はClaude Codeが未対応（2026-09時点、anthropics/claude-code issue #7108）。確認が要る操作は`confirm:true`引数で完結させ、無ければ「何をするか」を返して終わる
- エラーは構造化（原因・有効値の列挙・回復手順）。「`slot 5`は存在しない。存在するのは`1, 2, feat-x`。`slot_new`で作れる」の粒度

### 4.2 クライアント側の配線（Claude Code）

- 登録はuserスコープ（横断の道具なので）。`.mcp.json`に置いても番号は入らない
- SessionStartフックで`port-keeper env --format claude-env >> "$CLAUDE_ENV_FILE"`。`additionalContext`に「このセッションは`shop/2`。番号は覚えず`resolve_url`で引く」の一文
- 確認済み: `CLAUDE_ENV_FILE`と`additionalContext`は公式機能。未確認: MCP resourcesのsubscribe。設計はtoolsのみに依存

---

## 5. 品質・安全要件

### 5.1 脅威モデル

台帳はサービス列挙の結果そのものである。守れる範囲を正直に書く。

| 相手 | 例 | 守れるか |
| --- | --- | --- |
| A. 同じ端末の別ユーザー | 共用マシン、CI runner | 守れる。0600／0700 |
| B. 同じユーザー権限の悪意あるプロセス | 侵害されたnpmパッケージ | **守れない**。`lsof -i`で同じ情報が取れる。台帳を「lsof以上の地図」にしないのが唯一の対策 |
| C. 事故による漏えい | commit、スクリーンショット、チャット、エージェントのログ | 主戦場。5.2〜5.5 |
| D. ネットワーク越し | LAN、DNSリバインディング | 待受を持たないので到達不能 |
| E. エージェントの過剰開示・注入 | 全台帳の一覧化、ツール結果に混入した指示 | 最小開示と構造化で縮める（§4.1） |

### 5.2 データ分類

| 区分 | 内容 | 置き場所 |
| --- | --- | --- |
| 公開 | マニフェスト（名前・環境変数名・導出式） | リポジトリ |
| ローカル限定 | 台帳（番号、スロット名、作業コピーパス、確認日時、固定の理由文） | `~/.local/state/port-keeper/` 0600 |
| 描画物 | `.env.local`のブロック等 | リポジトリ内・gitignore必須。追跡済みなら描画を拒否 |
| 保持しない | パスワード、トークン、接続文字列の認証部、テナント名等の「その先」 | スキーマに列がない |

### 5.3 権限の最小化

- プロセスを殺さない。ファイアウォールやhostsを触らない
- 待受を持たない（TCPもUnixソケットも）。将来HTTPを足すなら`127.0.0.1`限定・0600ファイルのBearerトークン・Host/Origin検査・明示起動のみ・自動起動なし
- プール外は`pin`のみ。解放はLISTENなし確認後。破壊系MCPツールは既定で未登録
- 暗号化は既定でしない（相手Aは権限で足り、相手Bは鍵も読める。ディスク盗難はOS側）。将来オプション

### 5.4 ログ・監査

既定でファイルログなし。`--verbose`でも番号はマスク（`2xxxx`）。監査は台帳内`audit`に操作種別と対象名だけ。

### 5.5 自己点検（`doctor`）

権限／描画先のgitignore／MCP設定に番号やトークンが無いか／プールが一時ポート・慣習的番号と重ならないか／自身が待ち受けていないこと／staleと固定の件数。

---

## 6. テスト戦略

- **割当の性質テスト（property test）**: 任意のスロット作成・解放列に対して、有効なリース同士のポートが重ならない・全てプール内・同一スロットは解放まで同一ブロック
- **並行テスト**: N個のプロセスが同時に`slot new`しても重複しない（SQLiteの直列化とUNIQUE制約の実証）
- **実在確認**: 実際にlistenしたソケットを`hijacked`と判定する。IPv6のみのlistenも検出する
- **描画のゴールデン**: マニフェスト＋台帳スナップショット → 各formatの出力が固定文字列と一致。マーカーブロックの冪等更新
- **固定の横断調停**: 2プロジェクトが同じ番号を`pin`すると2つ目が所有者付きで拒否される
- **MCP層**: in-processトランスポートでtool callの統合テスト。`current_context`の出力に番号が含まれないことをスキーマで検証
- **doctor**: 追跡済みファイルへの描画が拒否される

---

## 7. マイルストーン

- **M0（台帳コア）**: スキーマ、プール、ブロック割当、bind確認、`init/slot/env/url/status/gc/pin/unpin/doctor`。30サービスのプロジェクトでスロット5以上が立ち、2プロジェクトの固定衝突が拒否される
- **M1（MCP）**: `mcp`サブコマンド、§4のツール、annotations、Claude Codeフック雛形。「shopのslot5のadminのURL」が1ツール呼び出しで返る
- **M2（配布）**: GoReleaser、インストールスクリプト、Glamaとawesome-mcp-serversへの掲載。新しい端末で5分で導入（当初の案にあったnpmラッパーは作らず、Homebrew tapとMCPレジストリ登録はM3へ移した。§9.2）
- **M3（需要駆動）**: Windowsの実在確認、Streamable HTTP（§5.3の条件付き）、既存プロキシへの連携例

---

## 8. 既知のリスク・割り切り

1. 確認から起動までの競合はゼロにできない → プールの位置と`hijacked`報告で緩和（§3.9）
2. 同一ユーザー権限の攻撃者には台帳を守れない → 台帳をlsof以上の地図にしない（§5.1）
3. MCP公式Go SDKのannotations・outputSchemaの実機動作は未確認 → M1冒頭で検証。不足ならTS SDKへの切替を判断
4. Windowsのbind挙動とパス規約は未検討 → M3
5. 固定（pin）が増えると設計の意味が薄れる → 横断調停と`doctor`の催促で圧をかけるが、最終的には利用者の運用次第

---

## 9. 実装ノート（追補）

### 9.1 2026-09-17: M0・M1 実装で確定した差分

- **スキーマ**: `blocks.kind`列は廃止。固定（pin）はブロックを持たず`leases.pinned_at`／`pin_reason`だけで表す。`slots.released_at`も廃止し、解放は行削除＋`audit`で記録
- **接続**: SQLiteは`SetMaxOpenConns(1)`＋`_txlock=immediate`。トランザクション内の読み取りは必ず`Tx`経由（別接続だと接続待ちでデッドロックする。実装中に踏んだ）
- **空きオフセットの選択でも実在確認**する。台帳外のLISTENがある番号は飛ばす。`reassign <service>`は直前の番号も飛ばして別番号へ移す
- **実在確認**: SO_REUSEADDRを外したbind試行（IPv4/IPv6）＋connect試行。BSD系ではSO_REUSEADDR付きだとワイルドカードLISTENを見逃す
- **`hijacked`判定**: LISTENプロセスのcwdが取得でき、かつ作業コピー外で、かつコンテナランタイム系コマンド名でないときだけ。それ以外は`active`
- **CLI**: フラグは位置引数の前後どちらでも受ける。`env --if-present`（マニフェスト無しならexit 0・無出力）を追加し、SessionStartフックはこれを使う
- **MCP**: 省略可能な入力は`omitempty`必須（SDKが入力スキーマを検証する）。`slot_release`のドライランは`dry_run: true`を返す
- **Go 1.25**が必要（依存の要求）
- **移行の手触り**（26サービスの実プロジェクト相当で試して追加）: `pin`は`service=port`の複数指定を受け、失敗した項目だけ飛ばして続行する。`unpin --all`で一括解除。`slot ls`の固定列は件数だけにし、明細は`--pins`で出す（26件を1列に並べると表が読めなかった）
- **紐づけの保護**: 既に別スロットに紐づいた作業コピーから`slot new`しても紐づけを奪わない（その場では`--slot`で扱う旨を表示）
- **設定ファイルの未知キーはエラー**（`stale_days`を`[mcp]`の下に書くと黙って無効になる事故を防ぐ）
- **`doctor`の自己待受検査**は`lsof`でコマンド名`port-keeper`のLISTENを探す実検査にした。`lsof`が無ければ「検査省略」と表示
- **Claude Codeフック**は`port-keeper hook claude`が担う（`claude-session-start`は別名）。stdinのJSON（`hook_event_name`、`cwd`、`new_cwd`）を読み、`$CLAUDE_ENV_FILE`に`export`行を追記し、SessionStartなら`hookSpecificOutput.additionalContext`、CwdChangedなら`systemMessage`のJSONを出す。スロット未作成なら作らず案内だけ返す。形式は公式hooksドキュメント（code.claude.com/docs/en/hooks、2026-09-17取得）で確認済み: `CLAUDE_ENV_FILE`は`export`文を`>>`で追記、SessionStartとCwdChangedの両方で使え、CwdChanged時は次のCwdChangedまで有効
- **リポジトリ構成の差**: MCP層は`internal/mcpserver`（`internal/mcp`ではない）。`internal/{config,app,cli,gitx}`が加わり、`npm/`は未作成
- **CLIの追加フラグ**（§3.10の表に無いもの）: `slot new --no-bind`、`slot ls --pins`、`slot rm --force/--cascade`、`env --stdout/--if-present`、`gc --yes`、`doctor --fix`、`pin --force`、`unpin --all`、`version`、グローバル`--slot`
- **セルフレビューで直したもの**（要修正4件）: dotenv描画への改行注入（値を必ず引用）、`render.host`の未検証（ループバック名に限定）、他スロットのブロック内番号の固定（`--force`なしは拒否。空きオフセット選択でも全リース・プール・deny・台帳外LISTENと突合）、プール縮小後に旧ブロックからプール外番号を貸す（プール再確認）。推奨のうち採用: MCP `status`を真に読み取り専用に、名前なし`slot_new`の冪等化、固定済みサービスの削除拒否、`env`後の`hijacked`再確認、`--`終端と余分な位置引数の拒否、symlinkディレクトリ経由の脱出拒否、複数OSプロセスの同時実行テスト
- **独立レビュー（文脈を共有しない別エージェント、2026-09-17）で直したもの**: 要修正3件＝未紐づけ作業コピーでの既定スロット共有（`RequireBound`で拒否）、`PORT_KEEPER_SLOT`が紐づけより優先される（解決順を変更）、Windowsビルド不能（`SO_REUSEADDR`の無効化をビルドタグで分離。CIにクロスビルドを追加）。推奨＝MCPツールに`cwd`引数（セッションが別作業コピーへ移っても追従）、リポジトリ消失後のスロットを`gc --yes`で台帳から直接解放、`env`の警告から`reassign`の誘導を外す、`status`のプロセス名を64文字に制限し`listener`フィールドとして返す、`.env.local`の一時ファイル＋renameによる原子的書き込み、`init`の`.gitignore`判定を`git check-ignore`に、`status`の`lsof`を1回に集約、`init --here`、`stale_days`の負値拒否、CIに`govulncheck`
- **未実装**: §5.5の「MCP設定ファイルに番号やトークンが無いか」の検査
- **未実施**（この9.1を書いた2026-09-17の時点）: Homebrew tap、npmラッパー、MCPレジストリ登録、README.ja.md、Windows検証。その後の結論は、次の9.2（初回配布の準備で確定した差分）にある

### 9.2 2026-09-18: 初回配布（v0.1.0）の準備で確定した差分

この節は、OpenSpecのchange`distribution-v0-1`（第三者が使える状態にするための初回配布）の実装中に決まったことをまとめる。経緯の全文は`openspec/changes/distribution-v0-1/design.md`（アーカイブ後は`openspec/changes/archive/`の下）にある。

- **台帳のスキーマの版**: 台帳（SQLiteファイル）のスキーマの版を、SQLiteの`PRAGMA user_version`（ファイルのヘッダにある整数）に記録する。現在の版は1（`ledger.SchemaVersion`）。開く順序は「版を読む → 自分の版より新しければ、何も書かずに`NewerSchemaError`で終了 → 0（未記録）ならスキーマを流して1を書く（同じトランザクション）→ 同じなら何もしない」。それまでは、開くたびに無条件で`CREATE TABLE IF NOT EXISTS`を流していた
  - 理由: 同じ端末に版の違うバイナリが同居しうる（インストールスクリプトで入れたものと`go install`で入れたもの、更新前から動き続けているMCPサーバーのプロセスと更新後のコマンド）。版の記録が無いと、古いバイナリが新しいスキーマの台帳を気づかずに読み書きする。第三者の端末に台帳ができたあとでは入れにくいので、初回公開の前に入れた
  - MCPサーバーは長く動き続けるので、開いたときだけでなく、ツール呼び出しのたびに版を読み直す（`Ledger.CheckSchema`）。起動の時点ですでに台帳が新しい場合は、サーバーは起動せず、標準エラーに同じ案内を出して終了する
  - スキーマを変えるときは、`SchemaVersion`を上げ、`migrate`に移行の段を足す
- **`render_env`の失敗がプロトコルエラーになっていた不具合を修正**: Go SDKは、ツールがエラー（`IsError`）を返す場合でも、構造化出力を出力スキーマで検証する。`render_env`の失敗時の出力は`env`（map）がnilで、JSONでは`null`になり、「objectではない」として`tools/call`全体が失敗していた。エージェントは、ツールのエラーなら文面を読んで立て直せるが、プロトコルエラーでは立て直せない。失敗時も空のmapを返すようにし、「プロジェクトの外で全ツールを呼び、どれもツールのエラーとして返ること」のテスト（`TestFailuresAreToolErrors`）を足した
  - 見つけた経緯: スキーマの版のテストで、全ツールをわざと失敗させたときに`render_env`だけがプロトコルエラーになった
  - 教訓: 出力の型にmapを足すときは、失敗時の出力でもnilにしない
- **npmラッパーは作らない**（9.1の「未実施」にあった項目の結論）。port-keeperはMCPサーバーだけで完結せず、Claude Codeのフックと人の操作がPATH上の`port-keeper`コマンドを必要とするので、`npx`の都度起動ではなく常設インストール（インストールスクリプト）を導入経路にする。同じ作者のatx-mcpでは、npmの6パッケージの初回手動公開や公開設定など、メンテナの手作業が多かったことも理由である
- **MCP公式レジストリへの登録は保留**。レジストリが受け付ける形式（npm／pypi／nuget／cargo／oci／mcpb）のうち、npmとoci（コンテナ）はこの節の理由で使えない。mcpb形式（GitHub Releaseに置くまとめファイル。旧称DXT）も採らない。調査（2026-09-18）で分かったこと: (1) `.mcpb`を導入できるクライアントはClaude Desktop（macOSとWindows）だけで、Claude Code、VS Code、Cursorには導入経路が無い。VS Codeは、mcpbしか持たないサーバーを一覧から落とす。(2) Claude Desktopには「開いているプロジェクトの作業ディレクトリ」が無く、起動されるサーバーの作業ディレクトリも文書化されていない。port-keeperはクライアントの作業ディレクトリからプロジェクトとスロットを解決するので、コンテナと同じ理由で成立しない。(3) コンパイル済みバイナリを同梱する種別（`server.type: "binary"`）は、macOSのClaude Desktopでは展開時に実行権限が落ちて起動しない不具合が未修理である（`modelcontextprotocol/mcpb`のissue #294）。(4) `.mcpb`の中のバイナリはPATHに入らないので、npmラッパーと同じく、版の違う2本のバイナリが1つの台帳を共有する。なお、当初の懸念だったCPU（amd64／arm64）の区別は、`server.json`の`packages`にOSとCPUごとの`.mcpb`を複数並べる方法で解決できる（Goの実例がレジストリに複数ある）ので、見送りの理由ではない。性質が合う形式は、レジストリに提案されている`go`形式（`go install`できるGoモジュールを登録する。バイナリはPATH上に常設される）で、2026-09-18時点では未マージである（`modelcontextprotocol/registry`のissue #1307とPR #1321）。再検討の条件は、そのPRのマージである。`server.json`はパッケージの記述を持たない下書きのまま置く
- **Homebrew tapは保留**。tap用の別リポジトリと、そこへ書き込む長期の認証トークンが要る。リリースの経路に保存したトークンを置かない、という今の構成を崩すので、要望が出てから検討する
- **コンテナイメージは配らない**（READMEのNon-goalsに追加）。port-keeperは、ホストのネットワークでの`bind`確認、ホストの`lsof`、クライアントの作業ディレクトリ、ホーム配下の台帳を直接見る。コンテナはこの4つをすべて隔離するので、機能が成立しない。MCP公式レジストリのoci形式での登録も、同じ理由で採らない
