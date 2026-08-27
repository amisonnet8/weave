# 6. Go資産連携

[← 目次](README.md) | [前へ: 5. アクターモデル](05-actors.md) | [次へ: 7. モジュール →](07-modules.md)

Weaveはコンパイル時にGoソースへ変換される言語なので、Goの構造体・関数を呼び出せます。ただし4章までの動的なオブジェクトとは違い、**宣言済みのものだけ**を扱えます(Go側の値は実行時にフィールドを追加できないため)。

## Go関数の宣言と呼び出し(`weave_spec.md` §15.1〜§15.2)

```weave
main = fn(args) {
	toUpper = gofunc("?strings.ToUpper")
	trimSpace = gofunc("?strings.TrimSpace")

	print(at(toUpper("hello, weave"), 0))   // "HELLO, WEAVE"
	print(at(trimSpace("  padded  "), 0))    // "padded"
	return 0
}
```

**Go関数・Goメソッドの呼び出しは、戻り値の個数によらず常に3・4章で見た`list`(連番の数値キーを持つオブジェクト)を返します。** Weaveには複数戻り値という概念が無いため、`at(list, index)`で個々の値を読み出します。`toUpper(...)`は本来1つの文字列しか返しませんが、それでも`at(..., 0)`で取り出す必要があります。すべての呼び出しはreflectionを経由します(16節)。

Go型のメソッドを呼びたい場合は`gotype`でメソッドテーブルを作ります。

```weave
GoFile = gotype("?os.File", {
	Close: gomethod("Close"),
	Name: gomethod("Name")
})
goOpen = gofunc("?os.Open", GoFile)
```

`gotype`/`gofunc`による宣言は、通常はプログラム冒頭にまとめて書きます。一度宣言してしまえば、それ以降のコードは3章のプロトタイプオブジェクトとGo資産を、`at(...)`で取り出した後は区別せず同じ`.method()`という書き方で扱えます——これも0章の統一原理がそのまま効いている一例です。

## プロトタイプの結びつけ(`weave_spec.md` §15.2)

`gofunc`/`gomethod`は任意の**第2引数**(`gotype(...)`で宣言済みの名前)を取れます。渡すと、返ってきたlistの**位置0**がそのGo構造体/ポインタ値であることをコンパイル時に記録し、`at(result, 0)`で取り出した値に対して静的に`.Method()`が使えるようになります——上の`goOpen`の例がまさにこれで、第2引数の`GoFile`が`at(result, 0)`で取り出した値を`GoFile`のメソッドテーブルへ結びつけます。

```weave
main = fn(args) {
	GoReader = gotype("?*strings.Reader", {
		len: gomethod("Len")
	})
	newReader = gofunc("?strings.NewReader", GoReader)

	result = newReader("hello")
	r = at(result, 0)
	print(at(r.len(), 0))   // 5(r.len()自身もlistを返す)
	return 0
}
```

エラー確認は宣言とは切り離されています。Goの`(値, error)`という戻り値の形を自動でチェックすることはなく、`raiseIfError(at(result, N))`のように呼び出し側が明示的にエラー位置を取り出して確認します。

```weave
readFile = gofunc("?os.ReadFile")
result = readFile("data.bin")
raiseIfError(at(result, 1))
print(at(result, 0))   // Goの[]byteが自動でWeaveの文字列になる
```

Weave側でGo関数の引数・戻り値の型を検証する仕組みはありません——渡した値がGo側の実際のパラメータ型と一致しない場合、reflectionの生のパニックでプログラムが停止します。渡す値の型は呼び出し側が正しく合わせる責任を持ちます。

## `govar`——Goのパッケージレベル変数を読む(`weave_spec.md` §15.4)

```weave
Stdout = govar("?os.Stdout")
writeString = gofunc("?io.WriteString")
result = writeString(Stdout, "hello\n")   // os.Stdoutへ直接書き込む
raiseIfError(at(result, 1))
```

`govar`は読み取り専用です。参照するたびに実際のGo変数の現在値をそのつど読み直す「ライブ」な参照で、宣言した瞬間の1回のスナップショットではありません。

## 制約(`weave_spec.md` §15.5)

- `gotype`/`gofunc`/`govar`の第1引数には、Go標準ライブラリ、またはWeaveのビルド成果物自身が既に参照している他のパッケージの関数・メソッド・変数・型しか、確実には解決できません
- Goの型を新しく構築すること(Goの構造体リテラルをWeave側から組み立てる)はできません——常に「既存のGo関数・メソッドを呼んで返ってきた値を受け渡す」形でしかGoの値に触れられません
- 呼び出したGo関数・メソッドが(エラー値を返すのではなく)Go自身の`panic`を起こした場合、Weave側で捕まえる方法はありません

## 演習

1. `strings.Repeat(s string, count int)`を`gofunc`で宣言し、`"ab"`を`3`回繰り返した`"ababab"`を表示してください。
2. `strconv.Atoi`(文字列を数値に変換する、`(int, error)`を返す)を`gofunc`で宣言し、`"42"`を数値へ変換して`1`を足した`43`を表示してください。

<details>
<summary>解答例</summary>

```weave
main = fn(args) {
	repeat = gofunc("?strings.Repeat")
	print(at(repeat("ab", 3), 0))   // "ababab"
	return 0
}
```

```weave
main = fn(args) {
	atoi = gofunc("?strconv.Atoi")
	result = atoi("42")
	raiseIfError(at(result, 1))
	print(at(result, 0) + 1)   // 43
	return 0
}
```

</details>

[← 目次](README.md) | [前へ: 5. アクターモデル](05-actors.md) | [次へ: 7. モジュール →](07-modules.md)
