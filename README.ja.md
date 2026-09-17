# port-keeper-mcp

[English](README.md) | **日本語** | [简体中文](README.zh-CN.md)

ローカル開発で使うポート番号の台帳と、その上に載せたMCPサーバーです。Goで書かれています。並列で動かすコーディングエージェントの1つ1つに専用のポート一式を渡すので、どのエージェントも、ほかのエージェントと同時にアプリケーション全体を起動できます。

## 何が困るのか

コーディングエージェントは、並列で動かしてこそ力を発揮します。エージェントが5つなら、ブランチも作業コピーも5つです。`git worktree`は、エージェントごとに別々のファイル一式を用意してくれます。しかし、別々のポートまでは用意してくれません。

Webサービスの開発は、ここで行き詰まります。どの作業コピーにも同じ`.env`が入っているので、どの作業コピーの開発サーバーもAPIもデータベースも、同じ番号を使おうとします。番号を取れるのは、最初に起動したエージェントだけです。残りのエージェントは`address already in use`で止まります。もっと悪い場合には、別のエージェントのサーバーとデータベースに対して、気づかないままテストを流します。

自分用のアプリケーション一式を起動できないエージェントは、自分の仕事を自分で確かめられません。できるのは、コードを書くこと、静的解析をかけること、純粋な関数の単体テストを回すことまでです。E2Eテストも、データベースを通すテストも走らせられません。それにはアプリケーションが動いている必要があり、そのためにはエージェント1つにつき1つの環境と、その環境のサービスの数だけのポートが要るからです。これが無いと、すべてのエージェントが、動くたった1つの環境の順番待ちをすることになります。小さなCLIツールの開発なら、それでも困りません。大きなWebサービスでは困ります。作業が並列で進むのは検証の手前までで、検証からは直列になり、エージェントを並列で動かす意味が消えてしまいます。

エージェントは同時に動き、互いに相談しません。だから、命名の約束事やwikiに書いたポート範囲の表では持ちこたえられません。ポートの割り当ては、プロトコルのように働く必要があります。つまり、どのエージェントも同じ1か所に問い合わせ、複数のエージェントが同時に問い合わせても正しい答えが返る仕組みです。

## port-keeperがすること

port-keeperは、その「1か所」です。

- **エージェント1つに環境1つ。** 作業コピーごとに**スロット**を割り当て、スロットごとに専用のポートの塊を貸し出します。2つの貸し出しが同じポートを持つことは、端末上のどのプロジェクトの間でもありません。複数のエージェントが同時に問い合わせても同じです。
- **エージェントは推測せずに問い合わせる。** エージェントは、MCP越しにサービスの名前からURLを得ます。セッションの開始時には、フックがそのスロットのポートをエージェントのシェルに渡します。作ったばかりのworktreeには、自分のスロットを持つまで、メインの作業コピーのポートを渡しません。
- **動かし続けるものが無い。** デーモンも、プロキシも、ネットワークの待ち受けもありません。どのコマンドも、台帳を開き、仕事をして、終了します。

プロジェクトごとに、小さなマニフェストでサービスを宣言します（書くのはサービス名と環境変数名だけで、番号は書きません）。port-keeperは各**スロット**（プロジェクトの並列環境。cloneでもgit worktreeでも、作業コピー1つにつき1スロット）にプールから連続したポートの塊を貸し出し、`.env`に書き込み、「`shop/5/admin`のURLは？」という問いにCLIからでもMCP越しでも答えます。あなたもコーディングエージェントも、ポート番号を覚える必要はもうありません。呼ばれたときだけ動き、台帳とプロジェクト内の指定ファイル以外には何も書かず、ネットワークの待ち受けを一切開きません。

設計の全文は[docs/DESIGN.md](docs/DESIGN.md)（日本語）にあります。

## インストール

port-keeperは、実行時の依存を持たない単一の静的バイナリです。`PATH`の通った場所に置いてください。シェルも、エージェントのフックも、MCPクライアントも、同じ`port-keeper`コマンドを呼びます。1回インストールすれば3つとも動き、端末にあるバージョンは常に1つになります。

```sh
# macOS／Linux
curl -fsSL https://raw.githubusercontent.com/gridhra/port-keeper-mcp/main/scripts/install.sh | sh
# Windows（PowerShell）。CIでビルドとクロスコンパイルはしていますが、Windowsの実機ではまだ検証していません
irm https://raw.githubusercontent.com/gridhra/port-keeper-mcp/main/scripts/install.ps1 | iex
```

スクリプトは、最新の[GitHub Release](https://github.com/gridhra/port-keeper-mcp/releases)からOSとCPUに合うアーカイブを選び、そのSHA-256がリリースの`checksums.txt`と一致しない限り何もインストールせず、`port-keeper`を`~/.local/bin`に置きます。`sudo`は求めません。置き場所は`PORT_KEEPER_INSTALL_DIR`で、バージョンは`PORT_KEEPER_VERSION`で指定できます。更新はもう一度実行するだけです。置き換えるのはバイナリ1つだけで、台帳と設定には触れません。

同じことを手作業で行い、さらにアーカイブが「このリポジトリのリリース用ワークフローが、タグの付いたソースからビルドしたもの」であることを確かめるには、次のようにします。

```sh
gh release download --repo gridhra/port-keeper-mcp --pattern '*darwin_arm64.tar.gz' --pattern checksums.txt
shasum -a 256 -c --ignore-missing checksums.txt
gh attestation verify port-keeper_*_darwin_arm64.tar.gz --repo gridhra/port-keeper-mcp
tar -xzf port-keeper_*_darwin_arm64.tar.gz port-keeper && mv port-keeper ~/.local/bin/
```

Goのツールチェーン（1.25以上）があるなら、次でも入ります。

```sh
go install github.com/gridhra/port-keeper-mcp/cmd/port-keeper@latest
```

コンテナイメージと`npx`ランチャーは、意図して用意していません。理由は[非目標](#コンテナイメージは配らない)にあります。

## Quickstart

```sh
cd your-project
port-keeper init          # port-keeper.toml（名前だけ）を書き、.env.localを.gitignoreに追加
$EDITOR port-keeper.toml  # ポートが必要なサービスごとに[[service]]を1つ
port-keeper env           # ブロックを借り、.env.localの管理ブロックを書く
port-keeper url web       # http://localhost:20000（--openを付けるとブラウザで開く）
```

同じプロジェクトの2つ目の作業コピーには、別のスロットが割り当てられます。

```sh
git worktree add ../your-project-hotfix -b hotfix && cd ../your-project-hotfix
port-keeper slot new      # この作業コピー用に新しいブロックを借りる
port-keeper env           # こちらの.env.localを書く。1つ目の作業コピーとは衝突しない
```

コーディングエージェントからも同じものが見えるようにします。

```sh
claude mcp add --scope user port-keeper -- port-keeper mcp
```

続けて、[コーディングエージェントとの連携](#コーディングエージェントとの連携)にあるフック設定と2行の指示を追加してください。

## よくある回避策がうまくいかない理由

- **オフセット式は破綻する。** 「基準ポート＋(スロット−1)×1000」は、2つのサービスの基準値がちょうど4000離れた瞬間に壊れます。スロット5がスロット1と衝突し、数千のポートを余らせたまま環境数は4で頭打ちになります。
- **プロジェクトをまたぐ衝突は、誰の担当でもない。** ノートPC上のプロジェクトはそれぞれ思い思いに1000番単位の範囲を取り、そのうえOSが慣習的な番号をいくつか先に押さえます（macOSではControl CenterのAirPlay受信が5000と7000を握っています）。後から起動したほうが負けます。
- **番号は漏れる。** 覚えるしかなかった番号だから、人は`localhost:3001`をチャットや文書やissueに貼ります。名前（`shop/3/admin`）は何も漏らさず、どの端末でも解決できます。
- **エージェントは当てずっぽうで決める。** サーバーが必要になったコーディングエージェントは8000か3000を選び、そこにあったものを踏み潰します。当て推量の代わりに、問い合わせる道具を渡しましょう。

## ユースケース

1. **5つ目の作業コピー、計算は不要**
   > 「hotfixブランチ用にこのプロジェクトをもう1つ立てて」
   `port-keeper slot new hotfix`が新しいブロックを借り、`port-keeper env`が`.env.local`の該当部分を書きます。あとはいつもの`mise run dev`で全部起動します。他の4スロットとも、この端末上の他のプロジェクトとも衝突しません。

2. **何も覚えずに正しい管理画面を開く**
   > 「スロット3の管理画面を開いて」
   `port-keeper url shop/3/admin --open`、またはエージェントからMCPツール`resolve_url`。1往復でURLが1本返ります。

3. **2つのアプリ環境でデータベースを共有する**
   > 「スロット2はスロット1のMySQLとメールキャッチャーを使って」
   `port-keeper slot new 2 --infra-from 1`。`tier = "infra"`を付けたサービスはスロット1のポートに解決され、それ以外は自分のポートを得ます。

4. **誰がポートを占拠しているか調べる**
   > 「APIが`address already in use`と言っている」
   `port-keeper status`が台帳と実際のLISTEN状態を突き合わせ、PIDを示します。LISTENしているプロセスの作業ディレクトリがこの作業コピーの外（かつコンテナランタイムではない）なら、そのリースは`hijacked`と表示され、そうでなければ単に`active`です。port-keeperはプロセスを殺しません。その代わり、`port-keeper reassign <service>`でそのサービスを空いているポートへ移せます。

5. **ブックマークを壊さずに既存プロジェクトを移行する**
   メインの環境だけ従来の番号のまま維持するには、`port-keeper pin web=3000 admin=3001 api=8080 --reason "docs and bookmarks"`。固定（pin）は台帳内の全プロジェクトを横断して調停されるので、2つのプロジェクトが同時に3000を主張することはできません。他のスロットはプールに移ります（そちらのブックマークは`port-keeper url`で引き直してください）。文書が`port-keeper url`を案内するようになったら、`doctor`が`unpin`を促します。

## コーディングエージェントとの連携

port-keeperは、特定のエージェントを前提にしていません。どのクライアントにも共通する契約は、次の2つのコマンドだけです。

- `port-keeper context --json`: いまどこにいるか（プロジェクト、スロット、この作業コピーが使える状態か）と、次に何をすべきか。ポート番号は含みません。
- `port-keeper env --format export --if-present`: 現在のスロットの環境変数を`export`行で。プロジェクト外では何も出力しません。

クライアント固有の処理はすべて、この2つの上に載るアダプタです。`port-keeper hook claude`はClaude Codeのフックの仕様に合わせたアダプタで、1ファイルに収まっています。他のエージェント向けのアダプタも、核に手を入れずに追加できます。

### Claude Code

MCPサーバーをユーザー単位で一度だけ登録します（設定に番号は入らず、コマンドだけです）。

```sh
claude mcp add --scope user port-keeper -- port-keeper mcp
```

`~/.claude/settings.json`にフックを追加します。`SessionStart`では、このスロットのポートをエージェントのシェルに書き出し、プロジェクトとスロットをClaudeのコンテキストに加えます。`CwdChanged`（Claudeが別の作業コピーへ`cd`したとき）では、書き出したポートをその作業コピーのスロットのものに差し替えます。プロジェクト外ではコマンドは何も出力せず終了コード0で終わるので、フックがセッションを失敗させることはありません。

```json
{
  "hooks": {
    "SessionStart": [
      { "hooks": [ { "type": "command", "command": "port-keeper hook claude" } ] }
    ],
    "CwdChanged": [
      { "hooks": [ { "type": "command", "command": "port-keeper hook claude" } ] }
    ]
  }
}
```

### エージェントに道具の存在を教える

エージェントがこの道具の存在を知らなければ役に立ちません。放っておくと、エージェントは`python -m http.server 8000`や`vite --port 3000`を起動して、そこにあったものを踏み潰します。**ユーザーレベル**の指示ファイル（Claude Codeなら`~/.claude/CLAUDE.md`。他のエージェントにも相当するものがあります）に次の数行を置けば止められます。端末上のすべてのプロジェクトに効くからです。エージェントへの指示文なので英語のまま載せます。

```markdown
## Local ports

Local dev ports on this machine are managed by port-keeper (MCP server `port-keeper`).
Never choose a port number yourself and never start a server on an ad-hoc port.
To find where something runs, call the `resolve_url` tool
(or run `port-keeper url <project>/<slot>/<service>`).
If a task needs a new port, add a service to `port-keeper.toml` and run `port-keeper env`.
Refer to services by name (`shop/3/admin`), never by number, in docs, issues and chat.
```

プロジェクト単位ではなくユーザー単位に置く理由: 厄介なのは、エージェントが一時ディレクトリや、まだマニフェストの無いプロジェクトで起動する野良サーバーです。プロジェクト単位のファイルはそこには届きません。

複数のエージェント（Claude Code、Codex、Cursorなど）を使うなら、同じ内容をそれぞれのグローバル指示に置いてください。MCPの登録はクライアントごと、台帳は共通です。

## 保証すること

- **2つのリースが同じポートを持つことはない。** 端末上の全プロジェクト・全スロットにわたって、固定した従来の番号も含めて保証します。台帳のUNIQUE制約と、割当ごとの書き込みトランザクションで担保しています。
- **プールの中だけ。** 割当は設定されたプール（既定20000〜31999）の外に出ません。プールは慣習的なポート、macOSのサービスが取るポート、OSの一時ポート範囲を避けています。プールの外に出る唯一の手段は、理由を添えた明示的な`pin`です。
- **安定。** スロットは解放するまで同じブロックを保ちます。再起動をまたいでも変わりません。
- **作業コピー1つにスロット1つ。** 自分のスロットを持たない作業コピーには、既定スロットのポートを渡しません。`env`、`url`、`status`、MCPツールは先に`slot new`を実行するよう求めます（メインのスロットを共有したいなら`--slot 1`を付けてください）。これが、`git worktree add`した直後の作業コピーがメインの作業コピーのポートでサーバーを起動してしまう事故を防ぎます。
- **待ち受けなし、デーモンなし。** どのコマンドも台帳を開き、仕事をして、終了します。port-keeperのどの部分にも、ネットワークからは到達できません。
- **秘密情報なし。** 台帳にはパスワード、トークン、接続文字列の列がありません。`.env`の書き出しが、gitで追跡されているファイルに書き込むこともありません。

## 非目標（意図的に作らないもの）

どれも意図して決めたことです。当てはまる機能要望を出す前に、この節を読んでください。ここに書いた理由が、そのまま回答です。その理由に反論する要望は、同じ機能を言い換えただけの要望より、はるかに役に立ちます。

### リバースプロキシ・名前付きホスト（`http://admin.shop.localhost`）は作らない

port-keeperの目的は、ポート番号について*考えなくて済む*ことであって、番号を*見なくて済む*ことではありません。`port-keeper url shop/5/admin`（またはツール`resolve_url`）が`http://localhost:23417`を返した時点で仕事は済んでいます。`--open`なら開くところまでやります。

プロキシは、すべてのスロットが依存する常駐プロセスを1つ増やします。それが止まると全環境に一斉に到達できなくなり、どんなポート衝突より悪い壊れ方です。macOSでは特権ポート（80／443）が要り、TLSやWebSocketの転送まで面倒を見ることになり、「port-keeperは待ち受けない」という規則にも反します。さらに、ブラウザはCookieやlocalStorageを*ポートを含む*オリジンごとに分けるので、ポートが違うことこそがスロット間のセッションを分けているのです。1つのホスト名の裏に隠せば、その分離が消えます。それでもきれいなホスト名が欲しいなら、`port-keeper env --format json`の出力を、それを得意とする既存のプロキシ（portless、localias、devenv）に渡してください。port-keeperがプロキシを持つことはありません。

### プロセス管理（start／stop／restart／kill）は作らない

port-keeperは番号を予約し、それが何番かを教えます。その番号でサーバーを誰が、いつ、どのスーパーバイザーの下で起動するかは、タスクランナー（`mise`、`just`、`direnv`、`docker compose`、IDE）の仕事です。port-keeperは「そのポートは使用中で、PIDはこれ」と報告しますが、プロセスを殺すことは決してありません。AIエージェントが呼べる道具が、誤操作やプロンプトインジェクションで別環境の開発サーバーを落とせてはいけないからです。

### デーモンは作らない

どのコマンドもSQLiteの台帳を開き、書き込みトランザクションの中で仕事をし、終了します。CLIやMCPのプロセスが同時に走っても二重割当は起きません。SQLiteが書き込みを直列化し、`port`列がUNIQUEだからです。デーモンを置けばpub/subやTTL付きのリースが手に入りますが、その代償は台帳への第二の経路（ソケットかHTTP）と、動かし続けなければならないものがもう1つ増えることです。いまの要件に、それは必要ありません。

### 台帳の共有・同期はしない

台帳は、*あなたの端末で*どのサービスがどのポートでLISTENしているかの一覧です。これは攻撃者が最初に調べ上げようとするものそのものです。だからローカルにモード0600で置き、同期もcommitもアップロードもしません。チームで共有するのはマニフェストで、そこには名前とテンプレートだけがあり、番号はありません。

### 台帳に秘密情報は入れない

パスワード、トークン、接続文字列の列はなく、今後も作りません。秘密情報が必要に見える機能があるなら、それはシークレットマネージャーの仕事です。

### プールの外への割当はしない

`pin`は既存プロジェクトの移行のためだけにあります。`--reason`が必須で、1プロジェクトにつき1スロットに限られ、台帳内の全プロジェクトを横断して調停されます。`unpin`するまで`doctor`が催促し続けます。新しいプロジェクトは固定すべきではありません。

### コンテナイメージは配らない

port-keeperは、あなたの端末に属する4つのものを直接見る必要があります。ホストのネットワーク（ポートが使われているかを、実際にbindして確かめます）、ホストのプロセス一覧（誰がLISTENしているかを`lsof`で調べます）、シェルやエージェントがいまいる作業ディレクトリ（そこからプロジェクトとスロットを見つけます）、そしてホームディレクトリの下の台帳です。コンテナは、まさにこの4つを隔離するためのものです。コンテナの中のport-keeperは、空のネットワーク名前空間を調べてすべてのポートを「空き」と報告し、プロジェクトを見つけられず、コンテナの終了と同時にリースを忘れます。macOSとWindowsでは、コンテナランタイム自体がLinuxの仮想マシンの中で動くので、ホストのネットワークを共有する指定をしても、開発サーバーが握っているポートには届きません。

Linuxなら、マウントやフラグで一部は取り繕えます。しかしその1つ1つがホストの一部をコンテナに渡すことになり、最後には隔離が何も残りません。だからイメージは配りませんし、port-keeperがOCIパッケージとしてどこかに掲載されることもありません。バイナリは単一の静的ファイルで、[インストール](#インストール)のコマンド1つで`PATH`に入ります。

`npx`ランチャーを用意しないのも、近い理由です。必要なときにサーバーを取得して起動するランチャーは、MCPクライアントにはサーバーを渡せますが、フックとシェルには`port-keeper`コマンドを渡せません。そのうえ、バージョンの違う2つのバイナリが1つの台帳を共有する状態を作ります。

### それでも欲しいなら

上の理由から出発し、そのどの部分があなたの状況では成り立たないかを書いたissueを立ててください。「便利だから」は織り込み済みです。答えを変えるのは、こちらが見落としていた失敗のしかたか、この非目標が本来の目的（ポート番号について考えないこと）を妨げている場面です。

## セキュリティモデル

port-keeperはローカルで完結する、ネットワークに出ない道具です。台帳はあなたの端末で何がどこでLISTENしているかの地図で、`~/.local/state/port-keeper/`にモード0600で置かれ、決して送信されません。同じ端末の別ユーザーに対してはこれで十分です。*あなた自身*として動くプロセスに対しては不十分で、ローカルの道具にそれを防ぐ手立てはありません。そのプロセスは既に`lsof -i`を実行できるからです。port-keeperがそこで行うのは、`lsof`より詳しい地図にならないこと（秘密情報なし、テナント名なし、短いラベル以外の説明なし）と、明示的に求められない限りエージェントに現在のプロジェクト以外を開示しないことです。`port-keeper doctor`が権限、`.gitignore`、port-keeper自身が待ち受けていないことを検査します。報告の方針と対象範囲は[SECURITY.md](SECURITY.md)を参照してください。

## リファレンス

### マニフェスト（`port-keeper.toml`）

リポジトリのルートに置き、commitする前提です。名前とテンプレートだけを持ち、番号はあなたのローカルの台帳にしかありません。

```toml
[project]
name = "shop"
block_size = 32          # スロットあたりのポート数。超えると2つ目のブロックが足される
slot_default = "1"       # 新しくcheckoutした作業コピーが使うスロット。固定（pin）できる唯一のスロット

[[service]]
name = "web"
env = "WEB_PORT"
proto = "http"
label = "Storefront"     # 任意・短く。エージェントに渡る前に64文字で切られる

[[service]]
name = "admin"
env = "ADMIN_PORT"
proto = "http"

[[service]]
name = "api"
env = "API_PORT"
proto = "http"

[[service]]
name = "db"
env = "DB_PORT"
proto = "tcp"            # tcpのサービスにはURLが無い
tier = "infra"           # `slot new --infra-from`で共有できる

[[derive]]               # ポートから組み立てる値。手で書く必要はない
env = "VITE_API_BASE"
value = "${url.api}"

[[derive]]
env = "ALLOWED_ORIGINS"
value = "${url.web},${url.admin}"

[render]
dotenv_path = ".env.local"   # gitignore必須。`port-keeper env`は追跡済みファイルを拒否する
dotenv_marker = "port-keeper" # 管理ブロックのマーカー文字列
host = "localhost"           # すべてのURLに使うホスト
```

テンプレート変数: `${port.<service>}`、`${url.<service>}`（http／httpsのサービスのみ）、`${slot}`、`${slot.infra}`、`${project}`、`${block.base}`。

### CLI

| コマンド | 役割 |
|---|---|
| `init [--name] [--here]` | マニフェストの雛形と`.gitignore`の行を書く。`--here`はgitのトップレベルではなく現在のディレクトリに書く（モノレポ向け） |
| `slot new [name] [--infra-from s] [--no-bind]` / `slot ls [--pins]` / `slot rm name [--force] [--cascade]` | スロットの作成・一覧・解放。`new`は、現在の作業コピーが別スロットに紐づいていない限り、そのスロットに紐づける |
| `env [--format f] [--stdout] [--if-present]` | 現在のスロットの環境変数を書き出す。`dotenv`（既定）は`.env.local`のマーカーブロックを書き換える。形式: `dotenv`、`export`、`json`、`mise`、`direnv`、`claude-env`（`export`の別名） |
| `url <service>` / `url <project>/<slot>/<service>` `[--open]` | URLを1本表示する（または開く） |
| `status` | 台帳と実際のLISTEN状態の突き合わせ |
| `context [--json] [--if-present]` | この作業コピーのプロジェクト、スロット、使える状態か、次にすべきこと。番号は含まない。エージェントやシェル向けの、クライアントに依存しない契約 |
| `gc [--yes]` | `stale_days`より長く使われていないスロットと、作業コピーが消えたスロットを一覧（または解放）する |
| `doctor [--fix]` | 権限、`.gitignore`、プールの妥当性、放置スロット、固定、自身の待ち受けが無いこと |
| `pin <service> <port> --reason t [--force]` または `pin web=3001 api=3002 --reason t` / `unpin <service>…` または `unpin --all` | 移行の補助。「保証すること」を参照。まとめて指定する形なら、従来の配置を1コマンドで固定できる |
| `reassign <service>` | サービスをプール内の別のポートへ移す（`status`が`hijacked`と報告した後に） |
| `mcp` | stdioでMCPを提供する |
| `hook claude` | Claude Codeの`SessionStart`と`CwdChanged`向けアダプタ。`context`と`env`を`$CLAUDE_ENV_FILE`とフックのJSONに変換する。プロジェクト外では無出力 |
| `version`（または`--version`） | バージョンを表示する |
| `--slot <name>` | グローバルフラグ。解決されたスロットではなく、指定したスロットに対して動く |

スロットは、`--slot`、現在の作業コピーに紐づくスロット、`PORT_KEEPER_SLOT`、マニフェストの`slot_default`の順で決まります。シェルに残った古い`PORT_KEEPER_SLOT`が紐づけと食い違うときは紐づけが勝ち、その旨の警告が出ます。

### MCPツール（8つ）

| ツール | 役割 |
|---|---|
| `current_context` | 作業ディレクトリから解決したプロジェクトとスロット、サービス名の一覧。番号は含まない（読み取り専用） |
| `resolve_url` | サービス1つの完全なURL。例: `http://localhost:23417`。他のプロジェクトを引くには`project`引数の明示が必要（読み取り専用） |
| `resolve_port` | サービス1つの生のポート番号（読み取り専用） |
| `render_env` | 現在のスロットの全環境変数を、指定の形式で。まだポートの無いサービスにはポートを貸す（冪等） |
| `status` | 現在のスロットについて台帳と実態の突き合わせ: `leased`（LISTENなし）、`active`、`stale`、`hijacked`。分かればLISTENしているPIDも（読み取り専用） |
| `slot_new` | 新しいスロットにブロックを貸す。名前が既に使われている、または作業コピーが既に紐づいている場合は既存のスロットを返す（冪等） |
| `slot_release` | スロットを解放する。何かがLISTEN中なら拒否。`confirm:true`が必要。**設定で有効化しない限り登録されない** |
| `list_all_projects` | 台帳内の全プロジェクトとスロットの名前。番号なし。**設定で有効化しない限り登録されない** |

すべてのツールは省略可能な`cwd`引数を受け付けるので、作業コピー間を移動するセッションでも正しいスロットが返り続けます。ポート番号を含むのは`resolve_url`、`resolve_port`、`render_env`だけで、いずれも問われた分だけです。他のツールは名前だけを返すので、エージェントの会話ログに番号が溜まりません。

### 設定（`~/.config/port-keeper/config.toml`）

すべて任意です。

```toml
stale_days = 30             # トップレベルのキー。どの[table]よりも前に置く

[pool]
ranges = [[20000, 31999]]
deny_ports = [27017, 28015, 29092]

[mcp]
enable_release = false      # trueにするとslot_releaseが登録される
enable_list_all = false     # trueにするとlist_all_projectsが登録される
```

未知のキーはエラーになるので、置き場所を間違えた設定が黙って無効になることはありません。

## 開発

Go 1.25以上（SQLiteドライバとMCP SDKが要求します。`GOTOOLCHAIN`が既定のままなら`go`がツールチェーンを自動で取得します）。

```sh
go test ./...                    # 単体・プロパティ・インプロセスMCPのテスト
go vet ./... && gofmt -l .
```

リリースは`v*`タグのpushで行います。手順は[RELEASING.md](RELEASING.md)にあります。`sh scripts/install_test.sh`はインストールスクリプトのテストです。

変更提案にはOpenSpec（`openspec/`）を使っています。`openspec list`で一覧できます。

パッケージ構成: `internal/config`（プール、パス）／`internal/manifest`／`internal/ledger`（SQLite、割当）／`internal/probe`（bindによる確認、LISTENの発見）／`internal/gitx`／`internal/render`（dotenv、export、json、mise、direnv、claude-env）／`internal/app`（解決、同期、状態、固定）／`internal/cli`（コマンド。`hook_claude.go`がClaude Code用アダプタ）／`internal/mcpserver`（stdioサーバー）／`cmd/port-keeper`。

## 名前について

*keeper*（番人）は鍵と台帳を預かり、どの扉がどれかを教える人です。扉を作ることも、代わりに開けることもしません。`-mcp`の接尾辞はMCPサーバーの命名慣習に倣ったもので、バイナリ名は単に`port-keeper`です。

## ライセンス

MIT。[LICENSE](LICENSE)を参照してください。
