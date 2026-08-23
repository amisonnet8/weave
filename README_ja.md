# Weave

プロパティ読み取り・メソッド呼び出し・アクターへのメッセージ送信を「名前によるプロパティ検索・プロトタイプチェーン」という1つの仕組みに統一した、動的型付けのプログラミング言語です。AMIVM-IRを経由してGoソースコードへコンパイルします(Go実装)。

> [English README is here](README.md)

## ステータス

Weaveのフロントエンド(字句解析・構文解析・意味検査・AMIVM-IRコード生成)は、[`weave_spec.md`](weave_spec.md)に記載された言語仕様を全て実装済みです: 動的な値(数値・文字列・真偽値・`nil`)、演算子、制御構文、関数・クロージャー・カリー化(自己再帰含む)、プロトタイプベースのオブジェクトとメソッド呼び出し、`[index]`による読み書き糖衣構文付きのlist(3.1節)、組み込み関数、`for`-`in`、アクターモデル(`spawn`/`send`/`ask`/`reply`)、任意のネイティブ型ヒント(`goReturns`/`goParams`・`govar`)を含む静的なGo資産連携(`gotype`/`gofunc`/`gomethod`、15〜16節)、Weave自身のオブジェクト向けの任意の実行時型検証(`shape`/`checkShape`、4.3節)、複数ファイル・複数パッケージのモジュール機構(`package(...)`、17節。`.wvz`圧縮パッケージ含む)。

## パイプライン

```
Weaveソース (.weave)
  ↓ (Weave — 本リポジトリ)
AMIVM-IR (.ir)
  ↓ amivm (外部ツール。github.com/amisonnet8/amivm)
Goソースコード (.go)
  ↓ go build
実行ファイル
```

Weave自身の責務はAMIVM-IRを出力するところまでです。それをGoソースへ変換するのは[amivm](https://github.com/amisonnet8/amivm)の仕事、実行ファイルにする単純な`go build`はさらに別工程で、どちらも`weave`が呼び出す外部ツールであり、本リポジトリ自体が実装しているものではありません。

## 動作要件

- Go([`go.mod`](go.mod)記載のバージョン)
- `PATH`の通った場所にインストールされた[`amivm`](https://github.com/amisonnet8/amivm)

## インストール

```sh
go install github.com/amisonnet8/amivm/cmd/amivm@latest
go install github.com/amisonnet8/weave/cmd/weave@latest
```

どちらも`$GOBIN`(未設定なら`$GOPATH/bin`)に配置されるので、そのディレクトリが`PATH`に通っていることを確認してください。Weaveのビルドは最終的に必ず素の`go build`で終わるため、Goさえインストールされていれば`weave`が実行時に必要とするものは全て揃います(それ以外に取得すべきものはありません)。

## 使い方

```
weave <コマンド> [フラグ] <file.weave | package-dir | package.wvz>
```

ディレクトリ(またはそれを圧縮した`.wvz`アーカイブ、仕様17.6節)を指定した場合、その中の全`.weave`ファイルを1つのパッケージとしてコンパイルします(`package(...)`、仕様17.2節)。単一ファイルを指定した場合はそのファイルだけをコンパイルし、同じディレクトリの他のファイルは無視します。

| コマンド | 出力 |
|---|---|
| `build` | 実行ファイル |
| `run` | コンパイルして即座に実行(stdin/stdout/stderrをそのまま引き継ぐ) |
| `emit-ir` | AMIVM-IR |
| `emit-go` | Goソースコード(amivm経由) |
| `wvz` | ディレクトリが実際にビルドできることを確認(成果物は破棄)してから`.wvz`アーカイブへまとめる |
| `help` | このコマンド一覧 |

`build`・`emit-ir`・`emit-go`は以下のフラグを受け付けます。

| フラグ | 説明 |
|---|---|
| `-o <file>` | 出力ファイルパス(省略時は入力パスから導出。例: `foo.weave` → `foo`/`foo.ir`/`foo.go`) |
| `-v` | 各パイプライン段階の出力を実行しながら表示(生成されたIR、amivm自身の`-v`トレース、最終的なGoソース) |

`wvz`は`-o <file>`(省略時は`<ディレクトリ名>.wvz`)を受け付けます。

## 例

```weave
main = fn(args) {
	base = {
		greet: fn(self) { print(self.name + " says hi") }
	}
	alice = { __proto__: base, name: "Alice" }
	alice.greet()

	add = fn(a) fn(b) { return a + b }
	print(add(5)(3))

	return 0
}
```

```sh
$ weave run hello.weave
Alice says hi
8
```

スカラー値・演算子・制御構文・クロージャー/カリー化/再帰・オブジェクト/プロトタイプ・組み込み関数/`for`-`in`・アクター・Go資産連携・複数パッケージのモジュールを一通り網羅した実行可能なサンプルを[`examples/`](examples/)に置いています。

## 言語仕様

**唯一の正確な仕様は[`weave_spec.md`](weave_spec.md)です。** 本READMEを含む他のドキュメントと矛盾する場合は`weave_spec.md`を優先してください。

## リポジトリ構成

```
cmd/weave/            CLIエントリポイント(本READMEの`weave`コマンド群)
internal/lexer/       字句解析
internal/parser/      構文解析 → AST
internal/ast/         AST定義
internal/modloader/   複数ファイル・複数パッケージのpackage(...)宣言(17節、ディレクトリまたは
                       .wvzアーカイブ)をsema/codegenの前に1つのフラットなASTへ解決する。
                       詳細はCLAUDE.md参照
internal/sema/        意味検査(スコープ解決・構文レベルの検査。Weaveは動的型付けのため、
                       値の型に依存するエラーの多くは実行時の関心事になる。詳細はCLAUDE.md参照)
internal/codegen/     AST → AMIVM-IR
weavert/              Weaveのランタイムライブラリ(全ての値が動的型のため、演算子・
                       オブジェクト・アクターはAMIVMネイティブ命令ではなくここを経由する。
                       ネイティブなクロージャー(入れ子のAMIVM CLOS)自体の呼び出しも
                       reflect経由でここを通る。詳細はCLAUDE.md参照)。weaveビルドの
                       たびに埋め込まれる
examples/             実行可能な.weaveサンプル(言語機能ごとにグループ化)
weave_spec.md         Weave言語仕様(唯一の正確な仕様)
weave_implementation_notes.md
                      このフロントエンドの実装で得た、AMIVM-IR生成に関する再利用可能な知見
                      (次にAMIVM上で言語を実装する人向け)
CLAUDE.md             AIによる開発支援のためのプロジェクト規約
```
