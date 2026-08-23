# 7. モジュール

[← 目次](README.md) | [前へ: 6. Go資産連携](06-go-interop.md) | [次へ: 8. まとめ →](08-wrapup.md)

ここまでのコード例は全て1ファイルで完結していましたが、Weaveは複数ファイル・複数パッケージにまたがるプログラムも書けます。

## パッケージ = ディレクトリ(`weave_spec.md` §17.1)

同じディレクトリに置かれた`.weave`ファイル群は、1つの「パッケージ」として1つのプログラムへ統合されます。パッケージ**内**のファイル間では、次の`package(...)`は不要です——全てのファイルが1つの共有スコープにあるかのようにコンパイルされます。

```
myproject/
    main.weave
    greeting.weave
```

```weave
// myproject/greeting.weave
greet = fn(name) {
	return "hello, " + name
}
```

```weave
// myproject/main.weave
main = fn(args) {
	print(greet("Weave"))   // greetingにpackage(...)せず直接呼べる
	return 0
}
```

`weave run myproject`のように**ディレクトリを指定**すると、この2つのファイルが1つのプログラムとしてまとめてコンパイルされます。`greeting.weave`で定義した`greet`に、`main.weave`側は何も宣言せずそのまま触れています——大文字始まりかどうかも関係ありません(次の節で見る、別ディレクトリのパッケージを`package(...)`で取り込む場合との違いです)。

一方、コンパイル対象として**単一ファイルを直接指定**した場合(`weave run myproject/main.weave`のように)は、そのファイル1つだけの独立したパッケージとして扱われます(同じディレクトリに他の`.weave`ファイルがあっても無視されます)——ここまでのツアーで1ファイルずつ`weave run`してきたのは、この扱いのおかげです。もし今、この章のここまでの例をファイルに保存して単体で`weave run`しようとしていたなら、`greeting.weave`を分けずに`greet`を同じファイルへ書く必要があります。

## パッケージをまたぐ参照 —— `package(...)`と大文字始まり公開(`weave_spec.md` §17.2)

別のディレクトリにあるパッケージを参照したい場合は、`gotype`/`gofunc`(6章)と全く同じ形——専用の構文やキーワードを新設せず、`名前 = package("<相対パス>")`という普通のトップレベルの束縛として書きます。

```weave
mathutil = package("./mathutil")

main = fn(args) {
	print(string(mathutil.Clamp(15, 0, 10)))
	return 0
}
```

```weave
// mathutil/clamp.weave
Clamp = fn(v, lo, hi) {
	if v < lo { return lo }
	if v > hi { return hi }
	return v
}

square = fn(x) { return x * x }   // 小文字始まりなので他パッケージから見えない

ClampSquare = fn(v, lo, hi) {
	return square(Clamp(v, lo, hi))
}
```

他パッケージから参照できるのは、**名前が大文字で始まるトップレベルの束縛だけ**です。値の種類は問いません——オブジェクト・関数・スカラー・`gotype`/`gofunc`宣言のいずれも、大文字で始めれば公開されます。

**`修飾子.名前`という参照は、3章の通常のプロパティアクセス/メソッド呼び出し糖衣構文とは別物であり、`self`の自動注入は一切起きません。** `mathutil.Clamp(15, 0, 10)`は`Clamp(15, 0, 10)`という素直な呼び出しに解決され、`Clamp(mathutil, 15, 0, 10)`のようにはなりません——`mathutil`はコンパイル時にのみ意味を持つ名前であり、実行時の値・オブジェクトではないためです。

## `.wvz`アーカイブ(`weave_spec.md` §17.6)

パッケージのソースは、実ディレクトリの代わりに1つの`.wvz`ファイル(ディレクトリをZIP圧縮しただけのアーカイブ)としてまとめて配布できます。

```sh
weave wvz ./mathutil   # ./mathutil.wvz を生成(ビルド確認込み)
```

```weave
mathutil = package("./mathutil.wvz")   // ディレクトリの代わりに.wvzを指す以外は同じ
```

`weave wvz`はアーカイブする前に、そのディレクトリのソースが実際にビルドできることを確認します(壊れたソースを圧縮してしまうことを防ぐため)。対象ディレクトリが自分自身のエントリーポイント(`main = fn(args) {...}`)を持つ場合はエラーになります——`.wvz`は常に`package(...)`で取り込まれる**パッケージ**の配布形式であり、実行可能プログラムの配布は`weave build`の役割だからです。

## 演習

`mathutil`パッケージ(上の例と同じ`Clamp`/`ClampSquare`)を自分のディレクトリに作り、ルート側から`mathutil.ClampSquare(6, 0, 10)`を呼んで結果(`Clamp(6, 0, 10) = 6`の2乗である`36`)を表示してください。

<details>
<summary>解答例</summary>

ディレクトリ構成:

```
myproject/
    main.weave
    mathutil/
        clamp.weave
```

`mathutil/clamp.weave`は本文の例と同じものを使います。

```weave
// myproject/main.weave
mathutil = package("./mathutil")

main = fn(args) {
	print(mathutil.ClampSquare(6, 0, 10))   // 36
	return 0
}
```

`weave run myproject/main.weave`(または`weave run myproject`)で実行します。

</details>

[← 目次](README.md) | [前へ: 6. Go資産連携](06-go-interop.md) | [次へ: 8. まとめ →](08-wrapup.md)
