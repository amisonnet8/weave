# Weave プロジェクト規約

## プロジェクト概要

Weaveは新規に設計する独自プログラミング言語。**Go言語で実装する**(Weaveコンパイラ自身の実装言語がGo)。ソースファイルの拡張子は**`.weave`**。AMIVM上に実装する3つ目のフロントエンド言語で、[[Seed]](素直な手続き型)・[[Cascade]](既存パラダイムの寄せ集め)に続き、Weaveは**前提を1つ疑うこと**を目的にした言語(詳細は`weave_spec.md` 0節)。

Weaveの核心は、次の3つを**1つの仕組み(名前によるプロパティ検索・プロトタイプチェーン)**の上に統一すること。

1. オブジェクトのプロパティ読み取り(`obj.x`)
2. プロトタイプ継承による関数呼び出し(`obj.greet()`)
3. アクターへのメッセージ送信(`send(a)("increment")(5)`)

アクターの「メッセージハンドラ」定義は、オブジェクトの「メソッド」定義と**全く同じ構文**(`fn(self, ...) {...}`をプロパティに持たせるだけ)。この統一原理がWeaveの設計の全てを貫く(コード生成レベルでも、メソッド呼び出しとメッセージディスパッチは同じヘルパーを共有する見込み。14節参照)。

Seed/Cascadeとの違いとして、Weaveは**動的型付け**言語であり(`weave_spec.md` 2節)、Seed/Cascadeが確立した「静的型をAMIVM-IRの型システムへ素直に落とす」パターンがそのままでは通用しない。値は実行時までAMIVMのネイティブ型(`^int`/`^string`等)に固定できず、多くの操作が`any`越しの動的処理になる可能性が高い(詳細は下記「Weave特有の設計課題」参照)。

コンパイルパイプラインはSeed/Cascadeと同じ3工程。

```
Weaveソース (.weave)
  ↓ (Weaveが担当。本リポジトリのスコープ)
AMIVM-IR (.ir)
  ↓ amivm (外部CLIツール。別リポジトリ)
Goコード (.go)
  ↓ go build (Weaveのビルドパイプラインが担当)
実行ファイル
```

**Weaveの責務は「Weaveソース → AMIVM-IR」の生成まで。** この3工程の境界を越えて責務を持たせない(WeaveがGoコードを直接生成する、AMIVM-IRの意味検証をWeave側でも二重に行う、といった実装はしない)。この原則は[[Seed]]・[[Cascade]]開発時と同じ。

## ドキュメント構成

| ファイル/ディレクトリ | 役割 |
|---|---|
| `weave_spec.md` | **Weave言語仕様。唯一の正確な仕様。** 字句規則・値と型・プロトタイプ・カリー化・アクターモデル・制御構文・Go資産利用などを定義。実装と齟齬が出たら、まず`weave_spec.md`の記述を疑い、仕様として確定してからコードを直すこと。特に14節・16節(AMIVM-IRへの対応方針)は実装方針の出発点だが、**あくまで仮説**であり実地検証で覆る可能性がある(下記「Weave特有の設計課題」参照) |
| `ignored/amivm/docs/amivm_spec.md` | AMIVM-IRの唯一の正確な仕様(下記「AMIVM-IRの書き方」節に要点を転記)。amivm本体のバージョンを上げた際は齟齬がないか確認すること |
| `ignored/seed_implementation_notes.md` | **[[Seed]]の実装で踏んだ地雷・確立したパターンのまとめ。実装前に必読。** goto/VAR巻き上げ問題(§1)は最重要。ポインタ・構造体・map・クロージャー・チャネル・ビット演算への「未実証命令への手がかり」(§5)は仮説段階の内容 |
| `ignored/cascade_implementation_notes.md` | **[[Cascade]]の実装で上記仮説を検証した結果のまとめ。実装前に必読。** Seedの推測がどこまで当たり、どこで外れたかが記録されている(特に§1のADDR、§3のmap走査、§4のクロージャー捕捉)。Weaveが新規に踏み込む領域(動的型・プロトタイプ検索・アクター)はCascadeにも無かった検証範囲のため、このnotesを読んでも解決しない論点が残る点に注意 |
| `ignored/seed/` | Seedの実装済みリポジトリ(参考実装)。ディレクトリ構成・レイヤー分割(`lexer`/`parser`/`ast`/`sema`/`codegen`)・CLIの作り・テスト戦略の実例として参照してよい。**Weaveリポジトリの一部ではない** |
| `ignored/cascade/` | Cascadeの実装済みリポジトリ(参考実装)。クロージャー・map・チャネル・パイプラインの実装パターンとして参照してよい。**Weaveリポジトリの一部ではない** |
| `ignored/amivm/` | 参照用にローカルへ置かれているamivmリポジトリのクローン。**Weaveリポジトリの一部ではない。** amivmはWeaveから見て「外部CLIツール」であり、`go install`で`PATH`に配置して呼び出す(下記参照) |
| 本ファイル(`CLAUDE.md`) | Weaveプロジェクトの規約・AIによる開発支援のための注意点 |

`ignored/`配下はgit管理対象外(`.gitignore`参照)。参照専用であり、Weave本体のビルド成果物やimportパスがここに依存することがあってはならない。

## amivmのインストール・呼び出し方

`amivm`はGo製CLIで、`go install`でインストールして`PATH`経由で呼ぶ(コピーやパス直接参照はしない)。

```sh
# ignored/amivm を使ってインストール、または本家リポジトリをclone後
cd ignored/amivm && make install   # go install ./cmd/amivm — $GOBIN(未設定なら$GOPATH/bin)に配置される
```

### CLIコマンド仕様

```
amivm <IRファイルパス> [-o|--output <出力ファイルパス>] [-v|--verbose] [-i|--import <名前>=<importパス>]...
```

- `-o`/`--output`省略時の出力先は、IRファイルパスの拡張子を`.go`に置き換えたパス
- `-v`/`--verbose`を付けると元のIR・型チェックの過程・最終的な生成コード・完了メッセージを標準出力に表示する
- `-i`/`--import <名前>=<importパス>`は繰り返し指定できる。**Weaveが独自のランタイムライブラリ(下記「独自のGoランタイムを呼ぶ」参照)を呼びたい場合はこれを使う。** 未使用の名前は自動的に取り除かれるため、同じマッピング一式を全IRファイルに使い回してよい
- ファイル読み込み失敗・IRパースエラー・型チェック失敗などのエラーは常に出力する
- `go build`による実行ファイル生成は行わない(別工程。Weave側のビルドパイプラインで実行する)

## AMIVM-IRの書き方(唯一の正確な仕様)

以下は`ignored/amivm/docs/amivm_spec.md`からの要点転記。**Weaveのコード生成部がAMIVM-IRを出力する際は、この命令セット・カテゴリ・Kind分類に厳密に従うこと。**

### 制約・前提条件

- `FUNC`はトップレベルのみに置ける(関数のネスト不可)。`STTYPE`・`CLOS`・`SEL`もネスト不可
- スライス・マップ・構造体・クロージャー(関数型)は、対応する`TYPE`系命令(`SLTYPE`/`MPTYPE`/`STTYPE`/`FNTYPE`/`CHTYPE`)で型を定義してから使う
- トークンの区切り文字は**タブ**。行頭のインデント用タブは無視。`//`で始まる行はコメントとして無視

### 識別子のプレフィックス

| 記号 | 意味 |
|---|---|
| `$` | 関数引数 |
| `&` | クロージャー引数 |
| `%` | 関数内変数名 |
| `@` | 関数外変数名(グローバル変数) |
| `^` | 型名 |
| `>` | 構造体フィールド名 |
| `!` | amivm定義関数名(`!xxx`→`<関数名>_amivm_function`、`!main`→`main`) |
| `?` | Go関数名(標準ライブラリ・Weave独自ランタイム問わず) |
| `#` | ラベル名 |

### 命令一覧(全カテゴリ)

| 分類 | 命令 |
|---|---|
| 変数宣言 | `VAR local type1` / `GVAR global type1` |
| 代入・ポインタ・配列 | `SET` `ASET` `AGET` `PSET` `PGET` `ADDR` |
| 算術 | `ADD` `SUB` `MUL` `DIV` `MOD` |
| ビット演算 | `BAND` `BOR` `BXOR` `BCLEAR` `BNOT` |
| シフト | `SHL` `SHR` |
| 論理演算 | `AND` `OR` `NOT` |
| 比較 | `EQ` `NEQ` `LT` `LTE` `GT` `GTE` |
| 文字列連結 | `CONCAT single slice1 slice2 ...` |
| ラベル・分岐 | `LABEL label` / `GOTO label` / `IF boolean1 label` |
| 関数定義 | `FUNC defname type1 ... : type3 ...` / `RET` / `ENDFUNC` |
| 関数呼び出し | `CALL multi1 ... : callname value1 ...` / `DEFER` / `SPAWN` |
| チャネル | `CHTYPE` `CHMAKE` `CHSEND` `CHRECV` |
| select | `SEL` `CASESEND` `CASERECV` `DEFAULT` `ENDSEL` |
| スライス | `SLTYPE` `SLMAKE` `SLICE` |
| 構造体 | `STTYPE` `FIELD` `ENDSTTYPE` `FSET` `FGET` |
| map | `MPTYPE` `MPMAKE` `MSET` `MGET` `MPKEYS` |
| クロージャー・関数型 | `FNTYPE` `CLOS` `ENDCLOS` |

`type1`等の「型」オペランドは`^xxx_123`のようなGo型名トークンなら何でも許容される(5節オペランドカテゴリ)。**`^any`はGoの組み込み型`any`としてそのまま通る**ため、`MPTYPE ^string ^any`(Weaveのオブジェクト表現、14節)は構文上問題なく成立する見込み——ただし実地検証(amivm→go buildが通るか)は未実施。

各命令の生成Goコード・オペランドカテゴリ(`whole`/`integer`/`value`/`single`/`multi`/`callname`等)・Kind分類は`ignored/amivm/docs/amivm_spec.md`の4〜6節を参照。**キャスト・組み込み関数は専用命令を持たず`CALL`に統合されている。**

## Weave特有の設計課題(実装前に確定すべき論点)

`weave_spec.md` 14節・16節はAMIVM-IRへの対応方針を示しているが、**Seed・Cascadeのどちらも通っていない領域(動的型付け・プロトタイプ検索・アクター)を含むため、仮説の域を出ない。** 実装ステップの早い段階で以下を実地検証し、確定した内容は本ファイルに「確定した設計判断」節を新設して記録すること(Seed/Cascadeと同じ運用)。

1. **動的型の値表現**: Weaveの値(数値・文字列・真偽値・`nil`・オブジェクト・関数・アクター参照)を、AMIVM-IR/Goのどの型に対応させるか。14節は「`any`越しの演算」を示唆しているが、`any`に格納する際の具体的な内部表現(数値は常に`float64`か、オブジェクトは`map[string]any`か、関数値は`FNTYPE`のクロージャーか、`any`の中にそれらをどう詰めるか)を先に決める必要がある。特に「オブジェクトのプロパティ値としてクロージャー・アクター参照・別のオブジェクトを混在させて`map[string]any`に入れられるか」はAMIVM側の型定義(`MPTYPE ^string ^any`)が許容するかどうかの実地検証が前提
2. **プロパティ検索(プロトタイプチェーン)の実装方式**: `obj.x`は`MGET`のcomma-ok形→見つからなければ`__proto__`を辿って再帰、という処理(14節)。これをAMIVM-IR自体で(`LABEL`+`GOTO`によるループ展開で)表現するか、Weave独自のGoランタイムヘルパー(`?weavert.GetProp(obj, name) any`のような1回の`CALL`)に肩代わりさせるかは未確定。後者の方がIR生成は単純だが、Cascade実装知見(cascade_implementation_notes.md §3)が示す通り「専用ランタイムが要るレベルの制約」に該当する可能性が高く、最初からランタイムヘルパー方式を軸に検討する価値がある
3. **カリー化のコンパイル時展開**: 多引数に見える定義・呼び出しを「1引数の`FNTYPE`+`CLOS`の連鎖」へ展開する変換自体はコード生成側の変換ロジックの問題であり新規AMIVM命令は不要と想定されるが、`fn(a, b) {...}`のような糖衣構文をASTのどの段階(parser/sema/codegen)で展開するかは設計判断が要る
4. **メソッド呼び出し糖衣構文**(`obj.method(a,b)` → `obj.method(obj,a,b)`、9節)の実装は上記2のプロパティ検索と共通のヘルパーを使う想定(0節の統一原理)。アクターのメッセージディスパッチ(6.2節)も同じヘルパーを共有できるかが、この言語の核心的な設計判断
5. **アクターのメッセージディスパッチ**: `spawn`(`CHTYPE`+`CHMAKE`+`SPAWN`)・メッセージ構造体(`STTYPE`で`{name string, args []any, replyTo chan any}`相当)・受信ループでの名前解決呼び出しの具体的なIR化パターンは未検証。`tell`/`ask`(6.3節)の一時受信チャネルの生成・破棄パターンも同様
6. **実行時型エラーの表現**: Seed/Cascadeは静的型付けのため`go/types`が大半のミスを捕捉したが、Weaveは動的型付けのため「数値+文字列は実行時エラー」(8節)のような検査を`go/types`に頼れず、**Weave自身のGoランタイム(型アサーション+panic、または値+errorのペア)で行う必要がある。** amivm側は型不整合を検証しない前提(意味検証の責任分担、下記参照)なので、動的型エラーの検出は実質的にランタイム層(コンパイル時のsemaではなく)に寄る可能性が高く、これはSeed/Cascadeの「semaが防波堤」という前提と根本的に異なる
7. **Go資産(`gotype`/`gofunc`/`gomethod`)の静的解決**(15〜16節): コンパイル時に評価される宣言であり、識別子が「動的なプロトタイプオブジェクト」か「静的に確定したGo型・Go関数への参照」かをsemaが区別する必要がある。この区別をASTレベル(識別子解決)でどう表現するか
8. **`main`のブリッジ**: Weaveの`func main(): int`もSeed/Cascadeと同じく`!main`に直接対応できない(引数無し・戻り値無しというGoの制約)可能性が高く、`weave_main`のような内部名分離パターンを踏襲する見込み(要確認)

## 意味検証の責任分担(重要)

型の整合性・未定義識別子・関数シグネチャの不一致・メソッドの存在チェックなどは、**amivm側では検証せず`go/types`に全面的に委ねている。** amivmが保証するのは「構文的に妥当なGoコードを出力すること」だけ。

Seed/Cascadeは静的型付け言語だったため、この防波堤としての役割をコンパイル時の`sema`パッケージが一手に引き受けられた。**Weaveは動的型付けのため、この前提が部分的に崩れる**(上記「Weave特有の設計課題」6参照)。Weaveのsemaが担うのは主にスコープ解決・構文レベルの検査(`gotype`/`gofunc`宣言の整合性、予約語の重複など)にとどまり、値の型に依存するエラー(数値+文字列の演算、存在しないプロパティへのメソッド呼び出し等)の多くは実行時にWeave自身のGoランタイムが検出することになる見込み。IRを間違って生成した場合のエラーがamivmの`go/types`型チェック失敗という分かりにくい形で返ってくる点はSeed/Cascadeと同じなので、**IR生成そのものの構文的正しさ(オペランドカテゴリ違反等)はamivm呼び出し前にWeave側で検査する。**

## 独自のGoランタイムを呼ぶ

`amivm`は`?pkg.Func`+`CALL`の仕組みで任意のGo関数を呼べる。Weaveは以下の理由から、Seed(`seedrt`)・Cascade(`cascadert`、現在は削除済み)よりも早い段階でGoランタイムパッケージ(`weavert`を想定)が必要になる可能性が高い。

1. プロトタイプチェーンを辿るプロパティ検索(上記課題2)
2. 動的型の演算・実行時型エラー(上記課題6)
3. `send`/`ask`/`reply`のメッセージハンドリング補助
4. `gofunc`/`gomethod`経由のGo値↔Weave値変換

導入する場合は、Seed(`seedrt/embed.go`)・Cascade(`cascadert/embed.go`、現在は削除)と同じ配布方式(`go:embed`で自身の`.go`ファイルを埋め込み、ビルド時にスクラッチディレクトリへコピーして`amivm`の`-i`で解決)を踏襲する。Cascadeの`cascadert`は`MPKEYS`命令の追加でネイティブ命令に置き換わり不要になった前例があるため、**「AMIVM本体の命令だけで実現できないか」を先に検討し、ランタイム関数化は最後の手段とする**(cascade_implementation_notes.md §8「AMIVM本体への機能要求が正当化される基準」も参照)。

## 過去に踏まれた地雷(Seed/Cascadeからの申し送り)

詳細は`ignored/seed_implementation_notes.md`・`ignored/cascade_implementation_notes.md`を参照。特に重要なものを抜粋。

1. **goto/VAR巻き上げ問題(最重要)**: `IF`/`GOTO`が生成するGoコードは1関数内のフラットな命令列であり、`goto`は「まだスコープに入っていない変数宣言」を飛び越えられない(Goのルール)。対象言語の関数・クロージャー・**アクターの受信ループ**ごとに、使う`VAR`を全て先頭に巻き上げ、初期化は元の位置に`SET`だけ残すこと
2. **スコープはGo側で完全にフラット**: シャドーイングを許す言語仕様(10節)に対して、codegenは内部で一意な変数名を採番すること
3. **`CALL`はキャストにも使われる**: `string(intVal)`のような変換はGoの素の型変換だと`"A"`のようなルーン変換になる罠がある(`strconv`を使うこと)
4. **`:=`ではなく常に`=`を使う**: `SEL`内の受信・`CHRECV`・その他あらゆる代入は、対象の変数が既に`VAR`/`GVAR`で宣言済みという前提。`:=`を使うとシャドーイングが起き、値が反映されない上に未使用変数の誤検出も引き起こす(amivm/CLAUDE.mdに記録されたamivm自身の実装バグと同じ罠)
5. **`CALL`の結果省略は本当に省略する**: 空文字列オペランドではなく`CALL : callname ...`のように空にする
6. **参照渡し/値渡しはGo表現に従う**: スライス・ポインタ・map・チャネル・関数値はコピーせずそのままローカル変数として使い回す。スカラーはコピーしてよい
7. **`STTYPE`/`CLOS`/`SEL`はネスト不可**。アクターの受信ループ内で新たなクロージャーやselectを使う設計にする場合、ネスト構造にならないよう注意
8. **`MGET`/`CHRECV`のcomma-ok形は値+成否フラグへ直結できる**(Cascade実地検証済み、cascade_implementation_notes.md §3・§5)。Weaveの`obj.x`が「無ければ`nil`」という意味論(4.2節)を実装する際、この形をそのまま使えるかが上記課題2の鍵になる
9. **クロージャーの変数捕捉は専用機構不要**: `CLOS`本体のスコープをクロージャーリテラル出現時点の外側スコープの子スコープにするだけで、コピーも専用のセル/ボックス機構も不要(cascade_implementation_notes.md §4)。Weaveのレキシカルスコープ(10節)もこのパターンをそのまま流用できる見込み
10. **map(オブジェクト)の走査は`MPKEYS`を使う**: Cascadeで一度`cascadert`ランタイムが必要になったが、`MPKEYS`命令の追加で不要になった前例がある(cascade_implementation_notes.md §3)。Weaveの`for k, v in obj`(7節)は最初から`MPKEYS`を使う設計にできる
11. **`callname`は`$N`/`&N`(関数引数・クロージャー引数)を直接受け付ける**(amivm改修済み、cascade_implementation_notes.md §4)。関数として受け取った値をそのまま呼ぶケースでコピー回避の特別処理は不要

## リポジトリ構成(予定・未実装)

**`/workspaces/weave`(このディレクトリ自体)がWeaveのホーム。** `weave_spec.md`・`seed_implementation_notes.md`・`cascade_implementation_notes.md`・`seed/`・`cascade/`・`amivm/`は`ignored/`配下にまとめて置き(git対象外)、Weave本体の実装(`go.mod`以下)はリポジトリ直下に追加していく。実装が進むにつれ実態に合わせて更新すること。

```
/workspaces/weave/              Weaveのホーム(このリポジトリのルート)
  weave_spec.md                 Weave言語仕様(唯一の正)
  CLAUDE.md                     本ファイル
  LICENSE                       MIT
  README.md / README_ja.md      導入ドキュメント(実装が固まってから作成)
  go.mod                        module github.com/amisonnet8/weave (想定)
  Makefile                      build/install/test/fmt/vet/tidy/clean タスク
  cmd/weave/
    main.go                     CLIエントリポイント(build/run/emit-ir/emit-go/help のディスパッチ)
    build.go                    compileToIR → compileToGo → compileToBinary の3段パイプライン
  internal/lexer/               字句解析
  internal/parser/               構文解析 → AST
  internal/ast/                 AST定義
  internal/sema/                意味検査(スコープ解決・構文レベルの検査。動的型のため範囲はSeed/Cascadeより狭い見込み)
  internal/codegen/             AST → AMIVM-IR生成
  weavert/                      Weave独自ランタイム(導入する場合。go:embedで配布)
  examples/                     サンプルWeaveプログラム(`.weave`。実装した構文ごとに追加)
  ignored/                      参照専用(git対象外)。amivm/seed/cascadeのクローンと各notes
```

## 実装ステップ計画(前半)

Weaveは3本柱(動的型付け・プロトタイプOOP・カリー化/アクター/Go資産連携)のうち難易度が高いため、開発を前半・後半に分ける。**前半は「動的型付け+プロトタイプOOP+カリー化」で言語の核を固めるところまでとし、アクターモデル(6節)とGo資産連携(15〜16節)は後半に回す。** 理由: アクターのメッセージディスパッチ(6.2節)はプロパティ検索と同じヘルパーを共有する設計(0節の統一原理)であり、まずプロパティ検索・メソッド呼び出しが正しく動くことを前半で確定させないと、後半のアクター実装が安全に組めない。Go資産連携も「動的なプロトタイプ検索」と対比される「静的解決」という性格上、動的な方を先に固めた方が設計しやすい。後半のステップ分けは前半完了後に改めて検討する。

各ステップは「機能単位の縦切り+都度amivm→go build→実行で実地検証」というSeed/Cascadeの開発プロセス(seed_implementation_notes.md §6.1)を踏襲する。

| # | ステップ | 主な内容 | 実証する命令(想定) | 解決する設計課題(上記「Weave特有の設計課題」の番号) |
|---|---|---|---|---|
| 1 ✅ | ブートストラップ | lexer/parser/ast/codegen最小構成。`func main(): int { print("Hello, Weave!") return 0 }`をamivm→go build→実行まで通す | `FUNC` `RET` `ENDFUNC` `CALL`(`print`) | 8. `main`のブリッジ方針を確定 |
| 2 | 動的型の値表現とスカラー変数 | `nil`/`true`/`false`/数値(float64統一)/文字列のリテラル・代入・`print`。Weaveの値をAMIVM-IR/Goのどの型に対応させるかを確定 | `VAR` `SET` `CALL` | **1. 動的型の値表現(最重要・最初に決める)** |
| 3 | 演算子 | 算術・比較・論理・文字列結合(8節)、優先順位表の実地検証。型が食い違う場合の実行時エラー(8節)を最初に実装し、検出方式を確定 | `ADD` `SUB` `MUL` `DIV` `MOD` `EQ` `NEQ` `LT` `LTE` `GT` `GTE` `AND` `OR` `NOT` `CONCAT` | 6. 実行時型エラーの表現 |
| 4 | 制御構文 | `if/elif/else`、`while`、`for`(範囲/カウンタ形。オブジェクト走査はStep6以降)、`break`/`continue` | `LABEL` `GOTO` `IF` | — (goto/VAR巻き上げ問題の再確認) |
| 5 | 関数・クロージャー・カリー化 | `fn(a) {...}`リテラル、レキシカルスコープ捕捉、多引数糖衣`fn(a,b)`のコンパイル時「1引数連鎖」展開、部分適用 | `FNTYPE` `CLOS` `ENDCLOS` | **3. カリー化のコンパイル時展開** |
| 6 | オブジェクトリテラルとプロパティアクセス(自身のみ) | `{x:1,y:2}`、`obj.x`読み書き、動的追加/削除、`has`。オブジェクトが数値・文字列・関数値を同じ`map`に混在して持てるかを実地検証 | `MPTYPE` `MPMAKE` `MSET` `MGET` | 1の続き(オブジェクト内での値表現)、2の前段(プロトタイプ無しの単純MGET) |
| 7 | プロトタイプチェーンとメソッド呼び出し糖衣構文 | `__proto__`を辿る再帰的プロパティ検索(4.2節)、`obj.method(a,b)`→`obj.method(obj,a,b)`展開(9節)。この2つが同じヘルパーを共有できるかを検証 | 6と同じ命令+ヘルパー関数呼び出し | **2. プロパティ検索の実装方式(最大の山場)**、**4. メソッド呼び出し糖衣構文** |
| 8 | 組み込み関数一式+`for-in`仕上げ | `for k,v in obj`(自身のプロパティのみ列挙)、`list`/`len`/`remove`/`has`/`string`を一式そろえる。継承・部分適用を使うサンプルで総仕上げ | `MPKEYS` | — |

**前半終了時点のゴール**: アクター・Go資産を使わず、プロトタイプ継承・カリー化・動的型だけで完結するWeaveプログラム(継承付きオブジェクトのメソッド呼び出し、部分適用)が実際に動く状態。

## 確定した設計判断

実装を進める中で確定した設計判断をここに記録する(Seed/Cascadeの同名節と同じ運用)。

### `main`のブリッジ方針(Step 1で確定)

Weaveの`func main(): int`をamivmの`!main`へ直接対応させることはできない(Goの`func main()`は引数無し・戻り値無しを要求するため)。**Seed(`!seed_main`)・Cascade(`!cascade_main`)と全く同じ解決策を採る**: ユーザーの`main`は内部名`weave_main`(`internal/codegen/codegen.go`の`weaveMainFunc`定数)としてコンパイルし、実際の`!main`は`weave_main`を呼んで戻り値を`os.Exit()`に渡す薄いラッパーとして生成する。現時点では`internal/sema`はまだ`weave_main`という名前のユーザー定義識別子を予約していない(Step1はmain以外の識別子束縛を持たないため該当ケースが無い)。**Step 5(関数・クロージャー)で名前束縛(`x = fn(a){...}`のような代入)が入った時点で、`weave_main`という名前をsemaで予約すること**(Seed/Cascadeの`seed_main`/`cascade_main`予約と同じ)。

### パーサの実装方式

**手書きの再帰下降パーサを採用する(Seed/Cascadeと同じ方針)。** パーサジェネレータは使わない。Step1時点では式の文法は「基本式+後置の関数呼び出し」のみで、演算子優先順位法(Pratt parsing)の本格導入はStep3(演算子)から。

### 数値リテラルの扱い

Weaveは整数・浮動小数点を言語レベルで区別しない(2節)ため、字句解析では`Number`という単一のトークン種別にまとめ(整数形・小数形どちらの見た目も同じKindとして扱う)、`ast.NumberLit`は常に`float64`で値を保持する。`main`の戻り値(exit code)はGoの`int`型が必要なため、`return <literal>`をコンパイルする際は「値に小数部が無いか」をcodegenが実行時ではなくコンパイル時に検査し、整数トークンとして`RET`へ埋め込む(Step2で一般の数値変数を導入する際、この「exit codeだけは整数チェックが要る」という特殊性が保たれるか要再検討)。

## 開発の進め方

1. `weave_spec.md`を正として実装する。仕様に曖昧な点や矛盾を見つけたら、まず仕様側を疑い、確定させてからコードを直す
2. 実装は機能単位の縦切りステップで進め、各ステップで実際に`amivm`(`PATH`にインストール済みのもの)にかけて`go build`まで通し、動作確認する(seed_implementation_notes.md §6.1の教訓)。特に上記「Weave特有の設計課題」1・2・5(動的型表現・プロパティ検索・アクター)は**Seed・Cascadeどちらでも実証されていない領域**なので、「ロジック上正しそうに見える」だけで次のステップへ進まないこと
3. 設計上の未確定事項(上記「Weave特有の設計課題」)は、着手時に方針を確定させ、確定内容を本ファイルに新設する「確定した設計判断」節(または実装コード側のdocコメント)に書き残す。仮説のまま放置しない
4. Weaveの意味検査(スコープ解決・構文検査)は、amivmに渡す前にWeave側で完了させる。amivmの`go/types`エラーをユーザー向けエラーとしてそのまま出さない。ただし動的型に起因する実行時エラーはこの限りではない(上記「意味検証の責任分担」参照)
5. 新しい構文・組み込み関数を実装したら、対応するサンプルWeaveプログラムを`examples/`に追加し、生成されたIR・Goコード・実行結果まで確認する
6. amivm本体の仕様が更新された場合(`ignored/amivm/docs/amivm_spec.md`を再確認)、本ファイルの「AMIVM-IRの書き方」節が古くなっていないか照合し、必要なら更新する
